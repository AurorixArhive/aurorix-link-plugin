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
	"github.com/KasumiYuku/Aurorix/lib/templates"

	"video-plugin/internal/detect"
	"video-plugin/internal/media"
	"video-plugin/internal/model"
	"video-plugin/internal/parse"
)

var log = logx.New("video")

const templateCard = "card"

// Config 是这一层读到的配置快照。
type Config struct {
	Enabled       map[string]bool
	GroupCooldown time.Duration
	LinkTTL       time.Duration
	Timeout       time.Duration
	SendVideo     bool
	MaxDuration   int
	MaxSizeMB     int
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
func (p *Pipeline) Handle(ctx *event.Context, e *event.MessageEvent) {
	hit, ok := hitOf(e)
	if !ok {
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
		p.fail(ctx, err)
		return
	}
	p.deliver(ctx, e, got, cfg)
}

func hitOf(e *event.MessageEvent) (detect.Hit, bool) {
	if hit, ok := detect.Find(e.Content); ok {
		return hit, true
	}
	if e.Ark != nil {
		return detect.FromArk(e.Ark.URLs())
	}
	return detect.Hit{}, false
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

	card, err := ctx.MarkdownTemplate(templateCard, &templates.Args{
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

	chain := ctx.Msg()
	if e.MessageID != "" {
		// 引用原消息: 群里同时可能有好几条链接, 不引用就不知道这条结果对的是谁发的
		chain.Quote(e.MessageID)
	}
	chain.Add(card)
	if videoPath != "" {
		chain.Video(videoPath)
	}
	for _, image := range got.Images {
		chain.Image(image, "图片")
	}
	if err := chain.Send(); err != nil {
		log.Warnf("发送失败: %v", err)
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

	file, written, err := media.Download(got.Video.URL, got.Video.Headers, limitBytes, 3*time.Minute)
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

func (p *Pipeline) fail(ctx *event.Context, err error) {
	msg, err := ctx.MarkdownTemplate("fail", &templates.Args{"reason": reason(err)})
	if err != nil {
		log.Warnf("渲染失败文案出错: %v", err)
		return
	}
	if err := msg.Send(); err != nil {
		log.Warnf("发送失败文案出错: %v", err)
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
