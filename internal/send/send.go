// Package send 把解析结果变成群里能看到的那条消息, 并负责防刷。
package send

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/event"
	"github.com/KasumiYuku/Aurorix/lib/logx"
	"github.com/KasumiYuku/Aurorix/lib/structers"
	"github.com/KasumiYuku/Aurorix/lib/templates"

	"video-plugin/internal/detect"
	"video-plugin/internal/media"
	"video-plugin/internal/model"
	"video-plugin/internal/parse"
)

var log = logx.New("video")

const (
	templateCard = "card"
	templateFail = "fail"
	templateNote = "note"
)

// Config 是这一层读到的配置快照。
type Config struct {
	Enabled map[string]bool
	// Cookies 按平台存登录态, 键是平台名; 需要登录的平台才用得上。
	Cookies       map[string]string
	GroupCooldown time.Duration
	LinkTTL       time.Duration
	Timeout       time.Duration
	SendVideo     bool
	MaxDuration   int
	MaxSizeMB     int
}

// Cookie 该平台配置的登录态; 没配置返回空串, 解析器按"匿名"处理。
func (c Config) Cookie(platform string) string {
	return c.Cookies[platform]
}

type Pipeline struct {
	parser *parse.Set
	cfg    func() Config

	mu     sync.Mutex
	recent map[string]time.Time
}

func New(parser *parse.Set, cfg func() Config) *Pipeline {
	return &Pipeline{parser: parser, cfg: cfg, recent: make(map[string]time.Time)}
}

// Handle 是消息事件入口: 认链接 → 防刷 → 解析 → 发送。
// 认不出链接的消息一律静默 —— 群里聊什么都能触发的机器人只会被踢。
// 但静默前必须留痕: 否则"发了链接没反应"在线上只能靠猜。
func (p *Pipeline) Handle(ctx *event.Context, e *event.MessageEvent) {
	hit, ok := hitOf(e)
	if !ok {
		traceUnmatched(e)
		return
	}
	cfg := p.cfg()
	if !cfg.Enabled[hit.Platform] {
		return
	}

	scope := e.GroupID
	if scope == "" {
		scope = e.UserID
	}
	if !p.claim("scope:"+scope, cfg.GroupCooldown) {
		return
	}
	if !p.claim("link:"+hit.URL, cfg.LinkTTL) {
		return
	}

	got, err := p.parse(hit, cfg)
	if err != nil {
		p.fail(ctx, e, err)
		return
	}
	p.deliver(ctx, e, got, cfg)
}

// traceUnmatched 记下"看着像卡片/转发/媒体, 却没认出链接"的消息。
//
// 普通聊天一个字都不记 —— 群里每句话都打一条日志, 日志里就什么都看不出来了。
// 真正需要留痕的只有那几种: 平台推了东西、我们没能识别出内容。
func traceUnmatched(e *event.MessageEvent) {
	if !worthTracing(e) {
		return
	}
	log.Debugf("没认出链接: msg_type=%d 元素=%d 附件=%d ark=%s urls=%v 正文=%.40q",
		e.MsgType, len(e.Elements), len(e.Attachments), arkLabel(e.Ark), e.Ark.URLs(), e.Content)
	if len(e.Raw) > 0 {
		// 平台推的形状框架没建模时, 只有原始载荷说得清到底收到了什么。
		log.Debugf("原始载荷: %.600s", e.Raw)
	}
}

// worthTracing 判断这条消息值不值得留痕: 卡片、转发、富媒体, 或者连正文都没有的空消息。
// 有正文的纯文本是正常聊天, 不看。
func worthTracing(e *event.MessageEvent) bool {
	if e.Ark != nil || len(e.Elements) > 0 || e.MsgType != 0 {
		return true
	}
	return e.Content == "" && len(e.Attachments) == 0
}

// hitOf 从这条消息里找第一个能解析的链接。
// 转发的卡片与合并转发: 正文和卡片都在消息元素里, 顶层 Content 与 Ark 都是空的。
func hitOf(e *event.MessageEvent) (detect.Hit, bool) {
	// 光秃秃的 BV 号只可能出现在正文里
	if hit, ok := detect.Find(e.Content); ok {
		return hit, true
	}
	if hit, ok := fromArk(e.Ark); ok {
		return hit, true
	}
	for _, el := range structers.FlattenElements(e.Elements) {
		if hit, ok := detect.Find(el.Content); ok {
			return hit, true
		}
		if hit, ok := fromArk(el.ArkData); ok {
			return hit, true
		}
	}
	return detect.Hit{}, false
}

func fromArk(ark *structers.ArkData) (detect.Hit, bool) {
	if ark == nil {
		return detect.Hit{}, false
	}
	return detect.FromArk(ark.URLs())
}

func arkLabel(ark *structers.ArkData) string {
	if ark == nil {
		return "-"
	}
	return ark.ArkType + "/" + ark.Prompt
}

func (p *Pipeline) parse(hit detect.Hit, cfg Config) (model.Media, error) {
	type outcome struct {
		media model.Media
		err   error
	}
	// 解析跑在独立协程里, 这样超时能立刻给用户回话; 解析器自身的请求超时会更早结束,
	// 所以这里的 goroutine 不会积压。
	done := make(chan outcome, 1)
	go func() {
		got, err := p.parser.Do(hit.Platform, hit.URL)
		done <- outcome{media: got, err: err}
	}()

	select {
	case got := <-done:
		return got.media, got.err
	case <-time.After(cfg.Timeout):
		return model.Media{}, errors.New("解析太久了, 稍后再发一次试试")
	}
}

// claim 记账并回答"这次能不能做": 同一个 key 在 ttl 内只放行一次。
// scope 用于群级冷却, link 用于同链接去重, 共用一张表。
func (p *Pipeline) claim(key string, ttl time.Duration) bool {
	if ttl <= 0 {
		return true
	}
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()
	if until, ok := p.recent[key]; ok && now.Before(until) {
		return false
	}
	if len(p.recent) > 1024 {
		for k, until := range p.recent {
			if now.After(until) {
				delete(p.recent, k)
			}
		}
	}
	p.recent[key] = now.Add(ttl)
	return true
}

func (p *Pipeline) deliver(ctx *event.Context, e *event.MessageEvent, got model.Media, cfg Config) {
	note, videoPath := p.videoPlan(got, cfg)
	if videoPath != "" {
		defer os.Remove(videoPath)
	}

	cardMsg, err := ctx.MarkdownTemplate(templateCard, &templates.Args{
		"emoji":    emoji(got),
		"platform": got.Platform,
		"title":    got.Title,
		"url":      got.PageURL,
		"meta":     metaLine(got),
		"desc":     got.Summary(),
		"note":     note,
	})
	if err != nil {
		log.Warnf("渲染卡片失败: %v", err)
		return
	}

	// 卡片与视频分成两条链发。视频要过 QQ 的 COS 分片上传, 是整条链路里最容易失败的一步;
	// 挂在同一条链上时它一失败, 这次发送整体算失败, 而用户那边看到的就是"发了链接没反应"。
	card := ctx.Msg()
	if e.MessageID != "" {
		// 引用原消息: 群里同时可能有好几条链接, 不引用就不知道这条结果对的是谁发的
		card.Quote(e.MessageID)
	}
	card.Add(cardMsg)
	for _, image := range got.Images {
		card.Image(image, "图片")
	}
	if err := card.Send(); err != nil {
		log.Warnf("发送卡片失败: %v", err)
		return
	}
	if videoPath == "" {
		return
	}

	video := ctx.Msg()
	if e.MessageID != "" {
		video.Quote(e.MessageID)
	}
	if err := video.Video(videoPath).Send(); err != nil {
		log.Warnf("发送视频失败: %v", err)
		p.say(ctx, e, templateNote, &templates.Args{"reason": "视频没传上去，点标题去原页面看"})
	}
}

// videoPlan 决定视频发不发本体: 超时长、超体积、下载失败都只发卡片, 并给一句说明。
func (p *Pipeline) videoPlan(got model.Media, cfg Config) (note string, path string) {
	if got.Video == nil || !cfg.SendVideo {
		return note, ""
	}
	limitBytes := int64(cfg.MaxSizeMB) << 20

	if cfg.MaxDuration > 0 && got.Duration > cfg.MaxDuration {
		return fmt.Sprintf("视频 %s，超过 %d 分钟上限，点标题去原页面看", durationText(got.Duration), cfg.MaxDuration/60), ""
	}
	size := got.Video.Size
	if size == 0 {
		size = media.ProbeSize(got.Video.URL, got.Video.Headers, 10*time.Second)
	}
	if size > limitBytes {
		return fmt.Sprintf("视频 %s，超过 %d MB 上限，点标题去原页面看", sizeText(size), cfg.MaxSizeMB), ""
	}

	file, written, err := media.Download(got.Video.URL, got.Video.Headers, limitBytes)
	if err != nil {
		if errors.Is(err, media.ErrTooLarge) {
			return fmt.Sprintf("视频超过 %d MB 上限，点标题去原页面看", cfg.MaxSizeMB), ""
		}
		log.Warnf("下载视频失败: %v", err)
		return "视频没下下来，点标题去原页面看", ""
	}
	log.Infof("已下载 %s (%s)", got.Title, sizeText(written))
	return "", file
}

func (p *Pipeline) fail(ctx *event.Context, e *event.MessageEvent, err error) {
	p.say(ctx, e, templateFail, &templates.Args{"reason": reason(err)})
}

// say 补一句话。失败必须让用户看得见 —— 只写日志的话, 用户那边就是"发了链接没反应"。
func (p *Pipeline) say(ctx *event.Context, e *event.MessageEvent, templateID string, args *templates.Args) {
	msg, err := ctx.MarkdownTemplate(templateID, args)
	if err != nil {
		log.Warnf("渲染 %s 文案出错: %v", templateID, err)
		return
	}
	chain := ctx.Msg()
	if e.MessageID != "" {
		chain.Quote(e.MessageID)
	}
	if err := chain.Add(msg).Send(); err != nil {
		log.Warnf("发送 %s 文案出错: %v", templateID, err)
	}
}

// reason 只把能看懂的话给用户: 领域错误本身就是给他看的, 其余一律换成不透明文案,
// 让用户对着 "context deadline exceeded" 发呆没有意义。
func reason(err error) string {
	if err == nil {
		return "解析没成功，稍后再试"
	}
	text := err.Error()
	if text == "" || strings.Contains(text, "http") || len([]rune(text)) > 60 {
		return "解析没成功，稍后再试"
	}
	return text
}

func emoji(got model.Media) string {
	switch got.Kind() {
	case "video":
		return "🎬"
	case "album":
		return "🖼"
	default:
		return "📄"
	}
}

func metaLine(got model.Media) string {
	parts := make([]string, 0, 4)
	if got.Author != "" {
		parts = append(parts, got.Author)
	}
	if text := durationText(got.Duration); text != "" {
		parts = append(parts, text)
	}
	if got.Stat != "" {
		parts = append(parts, got.Stat)
	}
	if len(got.Images) > 1 {
		parts = append(parts, fmt.Sprintf("%d 张图", len(got.Images)))
	}
	if len(parts) == 0 {
		return "点标题去原页面看"
	}
	return strings.Join(parts, " · ")
}

func durationText(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds%3600/60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func sizeText(bytes int64) string {
	if bytes >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	}
	return fmt.Sprintf("%d KB", bytes>>10)
}
