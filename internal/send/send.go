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
	templateNote = "note"
)

// skipNoticeGap 是"这次为什么没做"这类提示之间的最小间隔。
// 连着甩五条链接时跟着刷五句提示, 和刷屏没区别。
const skipNoticeGap = 30 * time.Second

// probeTimeout 探直链、问体积这类小请求的预算; 它们只取 1 字节, 慢就是不值得等。
const probeTimeout = 10 * time.Second

// directFailTTL 直传栽过一次之后, 这个平台多久内不再试直传。
// 让 QQ 去拉一条它拉不到的链, 白等十几秒之后还要再下一遍 —— 栽一次就够说明这条路走不通。
const directFailTTL = 30 * time.Minute

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
// 但"认出来了却决定不做"必须说话: 沉默和坏了在用户眼里是一回事。
func (p *Pipeline) Handle(ctx *event.Context, e *event.MessageEvent) {
	hit, ok := hitOf(e)
	if !ok {
		traceUnmatched(e)
		return
	}
	cfg := p.cfg()
	if !cfg.Enabled[hit.Platform] {
		log.Debugf("跳过 %s: 平台已在配置里关闭", hit.URL)
		return
	}

	scope := e.GroupID
	if scope == "" {
		scope = e.UserID
	}
	if !p.claim("scope:"+scope, cfg.GroupCooldown) {
		p.skip(ctx, e, scope, "刚处理过一条，稍等一下再发")
		return
	}
	if !p.claim("link:"+hit.URL, cfg.LinkTTL) {
		p.skip(ctx, e, scope, "这条链接 "+windowText(cfg.LinkTTL)+"内只解析一次，刚才已经发过了")
		return
	}

	got, err := p.parse(hit, cfg)
	if err != nil {
		// 解析失败一律静默: 用户要的是结果, 不是"没解析出来"这句解释。
		// 日志留着 —— 出问题时只有它能说话。
		p.release("link:" + hit.URL)
		log.Warnf("解析失败 %s: %v", hit.URL, err)
		return
	}
	if err := p.deliver(ctx, e, got, cfg); err != nil {
		// 卡片都没发出去等于这次什么都没给到, 同样不该占额度。
		p.release("link:" + hit.URL)
	}
}

// skip 告诉用户"这次为什么没做"。
//
// 提示本身也限频: 连着甩五条链接时跟着刷五句提示, 和刷屏没区别。
func (p *Pipeline) skip(ctx *event.Context, e *event.MessageEvent, scope, reason string) {
	log.Debugf("跳过 %.60q: %s", e.Content, reason)
	if !p.claim("skip:"+scope, skipNoticeGap) {
		return
	}
	p.say(ctx, e, templateNote, &templates.Args{"reason": reason})
}

// release 把额度还回去, 让下次还能重试。
func (p *Pipeline) release(key string) {
	p.mu.Lock()
	delete(p.recent, key)
	p.mu.Unlock()
}

// directFailKey 记"这个平台的直链交给 QQ 是拉不通的"。
func directFailKey(platform string) string { return "nofetch:" + platform }

// blocked 只读地问一句"这个 key 还在窗口里吗", 不占用额度。
func (p *Pipeline) blocked(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	until, ok := p.recent[key]
	return ok && time.Now().Before(until)
}

// windowText 把窗口时长说成人话。
func windowText(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	return fmt.Sprintf("%d 分钟", int(d.Minutes()))
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

// 平台给的消息类型里, 只有"合并转发(聊天记录)"的正文才在 msg_elements 里;
// 引用(103)的元素装的是被引用的那条消息, 是别人发的内容。
const msgTypeRecord = 102

// worthTracing 判断这条消息值不值得留痕。
//
// 只记"平台推了东西、我们却拿不到内容"的那几种: 卡片、合并转发, 以及连正文和附件都空的消息。
// 引用别人的消息只是正常聊天 —— 每句回复都记一条, 日志里就什么都看不出来了。
func worthTracing(e *event.MessageEvent) bool {
	if e.Ark != nil || e.MsgType == msgTypeRecord {
		return true
	}
	for _, el := range structers.FlattenElements(e.Elements) {
		if el.ArkData != nil {
			return true
		}
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

// deliver 发卡片与视频, 卡片没发出去时返回错误 —— 那等于这次什么都没给到, 调用方要还额度。
func (p *Pipeline) deliver(ctx *event.Context, e *event.MessageEvent, got model.Media, cfg Config) error {
	note, src := p.videoPlan(got, cfg)
	if src != "" && !isRemote(src) {
		defer os.Remove(src)
	}

	cardMsg, err := ctx.MarkdownTemplate(templateCard, &templates.Args{
		"emoji":    emoji(got),
		"platform": got.Platform,
		"title":    got.Title,
		"url":      got.PageURL,
		"author":   present(got.Author),
		"meta":     present(metaLine(got)),
		"desc":     descParagraph(got),
		"note":     present(note),
	})
	if err != nil {
		log.Warnf("渲染卡片失败: %v", err)
		return err
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
		return err
	}
	if src == "" {
		return nil
	}
	if err := p.sendVideo(ctx, e, src, got, cfg); err != nil {
		log.Warnf("发送视频失败: %v", err)
	}
	return nil
}

// sendVideo 发视频本体。来源是公网直链而平台自己拉不动时, 退回"自己下回来再传" ——
// 直传省掉云手机上最慢的两段路, 但它依赖平台服务器真能拉到那条链, 光靠探测下不了结论。
func (p *Pipeline) sendVideo(ctx *event.Context, e *event.MessageEvent, src string, got model.Media, cfg Config) error {
	err := p.pushVideo(ctx, e, src)
	if err == nil || !isRemote(src) {
		return err
	}
	log.Warnf("直传失败[%s], 改成自己下回来再传: %v", detect.Name(got.Platform), err)
	p.claim(directFailKey(got.Platform), directFailTTL)

	file, _, derr := media.Download(got.Video.URL, got.Video.Headers, int64(cfg.MaxSizeMB)<<20)
	if derr != nil {
		return err
	}
	defer os.Remove(file)
	return p.pushVideo(ctx, e, file)
}

func (p *Pipeline) pushVideo(ctx *event.Context, e *event.MessageEvent, src string) error {
	chain := ctx.Msg()
	if e.MessageID != "" {
		chain.Quote(e.MessageID)
	}
	return chain.Video(src).Send()
}

// isRemote 公网直链; 框架见到 http(s) 就把 URL 交给平台, 自己不下。
func isRemote(src string) bool {
	return strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")
}

// videoPlan 决定视频发不发本体: 超时长、超体积、下载失败都只发卡片, 并给一句说明。
// videoPlan 决定视频发不发本体, 以及从哪儿发。
// 超时长、超体积、下载失败都只发卡片, 并给一句说明。
func (p *Pipeline) videoPlan(got model.Media, cfg Config) (note string, src string) {
	if got.Video == nil || !cfg.SendVideo {
		return note, ""
	}
	limitBytes := int64(cfg.MaxSizeMB) << 20

	if cfg.MaxDuration > 0 && got.Duration > cfg.MaxDuration {
		return fmt.Sprintf("视频 %s，超过 %d 分钟上限，点标题去原页面看", durationText(got.Duration), cfg.MaxDuration/60), ""
	}
	size := got.Video.Size
	if size == 0 {
		size = media.ProbeSize(got.Video.URL, got.Video.Headers, probeTimeout)
	}
	if size > limitBytes {
		return fmt.Sprintf("视频 %s，超过 %d MB 上限，点标题去原页面看", sizeText(size), cfg.MaxSizeMB), ""
	}

	// 直链能被裸拉(不带任何请求头)时, 把 URL 交给 QQ 自己去下: 省掉云手机上行最慢的两段路。
	//
	// 但探测器看到的网和腾讯服务器看到的不是同一张 —— 实测出现过探测通过、QQ 十秒就报
	// "富媒体文件上传超时"。所以这个平台一旦栽过, 一段时间内不再试直传: 白等一场之后还得再下一遍。
	if !p.blocked(directFailKey(got.Platform)) && media.PubliclyFetchable(got.Video.URL, probeTimeout) {
		log.Infof("直链可直传[%s %s], 交给 QQ 拉取 %s", detect.Name(got.Platform), sizeLabel(size), got.Title)
		return "", got.Video.URL
	}

	file, written, err := media.Download(got.Video.URL, got.Video.Headers, limitBytes)
	if err != nil {
		if errors.Is(err, media.ErrTooLarge) {
			return fmt.Sprintf("视频超过 %d MB 上限，点标题去原页面看", cfg.MaxSizeMB), ""
		}
		log.Warnf("下载视频失败: %v", err)
		return "", ""
	}
	log.Infof("已下载 %s (%s)", got.Title, sizeText(written))
	return "", file
}

// sizeLabel 给日志用: 体积未知时直说, 别写成 "0 KB" 让人以为是真值。
func sizeLabel(bytes int64) string {
	if bytes <= 0 {
		return "体积未知"
	}
	return sizeText(bytes)
}

// say 补一句话。只在"认出来了却决定不做"时用 —— 那种情况不说话, 用户分不清是坏了还是被限流。
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

// metaLine 标题下面那行: 时长、播放、图集张数。
// 作者不在这里 —— 它跟着标题走, 见 card.md。
func metaLine(got model.Media) string {
	parts := make([]string, 0, 3)
	if text := durationText(got.Duration); text != "" {
		parts = append(parts, text)
	}
	if got.Stat != "" {
		parts = append(parts, got.Stat)
	}
	if len(got.Images) > 1 {
		parts = append(parts, fmt.Sprintf("%d 张图", len(got.Images)))
	}
	return strings.Join(parts, " · ")
}

// present 把一段可选文本包成 {{#each}} 吃得下的形态: 空数组就是"这段不出现"。
// 模板引擎没有条件语法, 这是它唯一能表达有无的方式 ——
// 换成塞占位文字, 卡片上就会多出一块空白引用或一句废话。
// 整行段落和"标题 · 作者"这种行内后缀用的都是它。
func present(text string) []any {
	text = strings.TrimSpace(text)
	if text == "" {
		return []any{}
	}
	return []any{map[string]any{"text": text}}
}

// descParagraph 简介段落; 和标题一字不差时不再重复一遍 ——
// 那看着像排版故障, 不像信息。
func descParagraph(got model.Media) []any {
	if strings.TrimSpace(got.Desc) == strings.TrimSpace(got.Title) {
		return []any{}
	}
	return present(got.Desc)
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
