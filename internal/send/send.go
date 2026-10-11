package send

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/event"
	"github.com/KasumiYuku/Aurorix/lib/logx"
	"github.com/KasumiYuku/Aurorix/lib/structers"
	"github.com/KasumiYuku/Aurorix/lib/templates"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/guard"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/media"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var log = logx.New("link")

const (
	templateCard = "card"
	templateNote = "note"
	voiceSeconds = 5 * 60
	// maxAudioBytes 是先下回来再传的上限: 超过就退回直传, 不把内存吃满。
	maxAudioBytes = 64 << 20
)

const skipNoticeGap = 30 * time.Second
const probeTimeout = 10 * time.Second
const directFailTTL = 30 * time.Minute

type Config struct {
	Enabled       map[string]bool
	Cookies       map[string]string
	GroupCooldown time.Duration
	LinkTTL       time.Duration
	Timeout       time.Duration
	SendVideo     bool
	MaxDuration   int
	MaxSizeMB     int
}

func (c Config) Cookie(platform string) string { return c.Cookies[platform] }

type Pipeline struct {
	engine *parse.Engine
	cfg    func() Config
	flight *guard.Flight[*model.Result]

	mu     sync.Mutex
	recent map[string]time.Time
}

func New(engine *parse.Engine, cfg func() Config) *Pipeline {
	return &Pipeline{
		engine: engine,
		cfg:    cfg,
		flight: guard.NewFlight[*model.Result](),
		recent: make(map[string]time.Time),
	}
}

func (p *Pipeline) Handle(ctx *event.Context, e *event.MessageEvent) {
	hit, ok := p.hitOf(e)
	if !ok {
		traceUnmatched(e)
		return
	}
	cfg := p.cfg()
	if !cfg.Enabled[hit.Parser.Platform().Name] {
		log.Debugf("跳过 %s: 平台已在配置里关闭", hit.Match.URL)
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
	if !p.claim("link:"+hit.Match.URL, cfg.LinkTTL) {
		p.skip(ctx, e, scope, "这条链接 "+windowText(cfg.LinkTTL)+"内只解析一次，刚才已经发过了")
		return
	}

	got, err := p.resolve(context.Background(), hit, cfg)
	if err != nil {
		p.release("link:" + hit.Match.URL)
		log.Warnf("解析失败 %s: %v", hit.Match.URL, err)
		return
	}
	if err := p.deliver(ctx, e, got, cfg); err != nil {
		p.release("link:" + hit.Match.URL)
	}
}

func (p *Pipeline) resolve(ctx context.Context, hit *parse.Hit, cfg Config) (*model.Result, error) {
	type outcome struct {
		got *model.Result
		err error
	}
	done := make(chan outcome, 1)
	key := hit.Parser.Platform().Name + ":" + hit.Match.URL
	go func() {
		got, err := p.flight.Do(key, func() (*model.Result, error) {
			return hit.Route.Handle(ctx, hit.Match)
		})
		done <- outcome{got: got, err: err}
	}()

	select {
	case out := <-done:
		return out.got, out.err
	case <-time.After(cfg.Timeout):
		return nil, errors.New("解析太久了, 稍后再发一次试试")
	}
}

func (p *Pipeline) hitOf(e *event.MessageEvent) (*parse.Hit, bool) {
	if hit, ok := p.engine.Match(e.Content); ok {
		return hit, true
	}
	if hit, ok := p.engine.FromArk(e.Ark.URLs()); ok {
		return hit, true
	}
	for _, el := range structers.FlattenElements(e.Elements) {
		if hit, ok := p.engine.Match(el.Content); ok {
			return hit, true
		}
		if el.ArkData != nil {
			if hit, ok := p.engine.FromArk(el.ArkData.URLs()); ok {
				return hit, true
			}
		}
	}
	return nil, false
}

func (p *Pipeline) deliver(ctx *event.Context, e *event.MessageEvent, got *model.Result, cfg Config) error {
	note, src := p.videoPlan(got, cfg)
	if src != "" && !isRemote(src) {
		defer os.Remove(src)
	}

	cardMsg, err := ctx.MarkdownTemplate(templateCard, &templates.Args{
		"emoji":    emoji(got),
		"platform": got.Platform.DisplayName,
		"title":    got.Title,
		"url":      got.URL,
		"author":   present(authorLine(got)),
		"meta":     present(metaLine(got)),
		"desc":     descParagraph(got),
		"note":     present(note),
	})
	if err != nil {
		log.Warnf("渲染卡片失败: %v", err)
		return err
	}

	card := ctx.Msg()
	if e.MessageID != "" {
		card.Quote(e.MessageID)
	}
	card.Add(cardMsg)
	for _, image := range cardImages(got) {
		card.Image(httpsImage(image), "图片")
	}
	if err := card.Send(); err != nil {
		log.Warnf("发送卡片失败: %v", err)
		return err
	}

	if err := p.sendAudios(ctx, e, got); err != nil {
		log.Warnf("发送音频失败: %v", err)
	}
	if src != "" {
		if err := p.sendVideo(ctx, e, src, got, cfg); err != nil {
			log.Warnf("发送视频失败: %v", err)
		}
	}
	return nil
}

func (p *Pipeline) sendAudios(ctx *event.Context, e *event.MessageEvent, got *model.Result) error {
	var last error
	for _, item := range got.Audios() {
		if err := p.sendAudio(ctx, e, item, got); err != nil {
			last = err
		}
	}
	return last
}

func (p *Pipeline) sendAudio(ctx *event.Context, e *event.MessageEvent, item model.Audio, got *model.Result) error {
	if item.Duration > voiceSeconds {
		return p.sendAudioFile(ctx, e, item, got)
	}
	chain := ctx.Msg()
	if e.MessageID != "" {
		chain.Quote(e.MessageID)
	}
	if err := chain.Voice(item.URL).Send(); err != nil {
		// QQ 的语音只收 silk/mp3/wav, 别的格式会失败 —— 退回发文件
		log.Warnf("发语音失败, 退回发文件: %v", err)
		return p.sendAudioFile(ctx, e, item, got)
	}
	return nil
}

// sendAudioFile 先下回来再按字节上传: 走 URL 直传时 QQ 会无视 file_name,
// 拿整条 URL 当文件名(query 也一起塞进去), 群里收到的就成了 "....m4a_guid=...&vkey=..."。
func (p *Pipeline) sendAudioFile(ctx *event.Context, e *event.MessageEvent, item model.Audio, got *model.Result) error {
	chain := ctx.Msg()
	if e.MessageID != "" {
		chain.Quote(e.MessageID)
	}
	name := audioName(item, got)
	file, _, err := media.Download(item.URL, item.Headers, maxAudioBytes)
	if err != nil {
		log.Warnf("下载音频失败, 退回直传: %v", err)
		return chain.File(item.URL, name).Send()
	}
	defer os.Remove(file)
	return chain.File("file://"+file, name).Send()
}

func (p *Pipeline) sendVideo(ctx *event.Context, e *event.MessageEvent, src string, got *model.Result, cfg Config) error {
	err := p.pushVideo(ctx, e, src)
	if err == nil || !isRemote(src) {
		return err
	}
	log.Warnf("直传失败[%s], 改成自己下回来再传: %v", got.Platform.DisplayName, err)
	p.claim(directFailKey(got.Platform.Name), directFailTTL)

	videos := got.Videos()
	if len(videos) == 0 {
		return err
	}
	file, _, derr := media.Download(videos[0].URL, videos[0].Headers, int64(cfg.MaxSizeMB)<<20)
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

func (p *Pipeline) videoPlan(got *model.Result, cfg Config) (string, string) {
	videos := got.Videos()
	if len(videos) == 0 || !cfg.SendVideo {
		return "", ""
	}
	video := videos[0]
	limitBytes := int64(cfg.MaxSizeMB) << 20

	if cfg.MaxDuration > 0 && video.Duration > float64(cfg.MaxDuration) {
		return fmt.Sprintf("视频 %s，超过 %d 分钟上限，点标题去原页面看", durationText(int(video.Duration)), cfg.MaxDuration/60), ""
	}
	size := media.ProbeSize(video.URL, video.Headers, probeTimeout)
	if size > limitBytes {
		return fmt.Sprintf("视频 %s，超过 %d MB 上限，点标题去原页面看", sizeText(size), cfg.MaxSizeMB), ""
	}
	if !p.blocked(directFailKey(got.Platform.Name)) && directUploadOK(got.Platform.Name, video.URL) && media.PubliclyFetchable(video.URL, probeTimeout) {
		log.Infof("直链可直传[%s %s], 交给 QQ 拉取 %s", got.Platform.DisplayName, sizeLabel(size), got.Title)
		return "", video.URL
	}

	file, written, err := media.Download(video.URL, video.Headers, limitBytes)
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

func (p *Pipeline) skip(ctx *event.Context, e *event.MessageEvent, scope, reason string) {
	log.Debugf("跳过 %.60q: %s", e.Content, reason)
	if !p.claim("skip:"+scope, skipNoticeGap) {
		return
	}
	p.say(ctx, e, templateNote, &templates.Args{"reason": reason})
}

func (p *Pipeline) release(key string) {
	p.mu.Lock()
	delete(p.recent, key)
	p.mu.Unlock()
}

func (p *Pipeline) blocked(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	until, ok := p.recent[key]
	return ok && time.Now().Before(until)
}

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

func directFailKey(platform string) string { return "nofetch:" + platform }

// httpsImage 把 http 直链抬成 https: 框架对公网 URL 是"直通"给 QQ 的, 不走图床,
// 而 QQ 只把 https 的地址当图渲染, 拿到 http 就原样显示成一条链接。
func httpsImage(rawURL string) string {
	if rest, ok := strings.CutPrefix(rawURL, "http://"); ok {
		return "https://" + rest
	}
	return rawURL
}

// cardImages 卡片带的图: 图集就是那几张; 视频帖本身没有图, 把封面挂上去 ——
// 不挂的话标题下面空着一块, 看着就像解析坏了。
func cardImages(got *model.Result) []string {
	if images := got.Images(); len(images) > 0 {
		return images
	}
	if cover := got.Cover(); cover != "" {
		return []string{cover}
	}
	return nil
}

// directUploadOK 决定这条直链敢不敢交给 QQ 去拉: 它拉得动与否我们事后看不见, 只能事前保守。
// 只认 https —— 实测把 http 直链递过去, 接口回成功、群里却没有视频(小红书和快手的 CDN 都发 http)。
// 快手另外拉黑: 它那条链也栽过, 而自己下回来传虽然慢, 至少一定发得出去。
func directUploadOK(platform, rawURL string) bool {
	if platform == "kuaishou" {
		return false
	}
	return strings.HasPrefix(rawURL, "https://")
}

func isRemote(src string) bool {
	return strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")
}

func windowText(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	return fmt.Sprintf("%d 分钟", int(d.Minutes()))
}

func emoji(got *model.Result) string {
	switch {
	case len(got.Videos()) > 0:
		return "🎬"
	case len(got.Audios()) > 0:
		return "🎵"
	case len(got.Images()) > 0:
		return "🖼"
	default:
		return "📄"
	}
}

func authorLine(got *model.Result) string {
	if got.Author.Name == "" {
		return ""
	}
	return got.Author.Name
}

func metaLine(got *model.Result) string {
	parts := make([]string, 0, 4)
	if videos := got.Videos(); len(videos) > 0 {
		if text := durationText(int(videos[0].Duration)); text != "" {
			parts = append(parts, text)
		}
	}
	if got.Stats.View != "" {
		parts = append(parts, got.Stats.View+" 播放")
	}
	if got.Stats.Like != "" {
		parts = append(parts, got.Stats.Like+" 点赞")
	}
	if images := got.Images(); len(images) > 1 {
		parts = append(parts, fmt.Sprintf("%d 张图", len(images)))
	}
	return strings.Join(parts, " · ")
}

func descParagraph(got *model.Result) []any {
	desc := strings.Join(got.Texts(), "\n")
	if strings.TrimSpace(desc) == strings.TrimSpace(got.Title) {
		return []any{}
	}
	return present(desc)
}

func present(text string) []any {
	text = strings.TrimSpace(text)
	if text == "" {
		return []any{}
	}
	return []any{map[string]any{"text": text}}
}

func audioName(item model.Audio, got *model.Result) string {
	name := strings.TrimSpace(item.Title)
	if name == "" {
		name = strings.TrimSpace(got.Title)
	}
	if name == "" {
		name = "audio"
	}
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_").Replace(name)
	return name + audioExt(item.URL)
}

// audioExt 后缀按直链的真实格式来: 一律写 .mp3 会让 m4a/flac 被播放器认错。
func audioExt(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	switch ext := strings.ToLower(path.Ext(rawURL)); ext {
	case ".mp3", ".m4a", ".flac", ".wav", ".aac", ".ogg", ".ape":
		return ext
	}
	return ".mp3"
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

func sizeLabel(bytes int64) string {
	if bytes <= 0 {
		return "体积未知"
	}
	return sizeText(bytes)
}

const msgTypeRecord = 102

func traceUnmatched(e *event.MessageEvent) {
	if !worthTracing(e) {
		return
	}
	log.Debugf("没认出链接: msg_type=%d 元素=%d 附件=%d ark=%s urls=%v 正文=%.40q",
		e.MsgType, len(e.Elements), len(e.Attachments), arkLabel(e.Ark), e.Ark.URLs(), e.Content)
	if len(e.Raw) > 0 {
		log.Debugf("原始载荷: %.600s", e.Raw)
	}
}

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

func arkLabel(ark *structers.ArkData) string {
	if ark == nil {
		return "-"
	}
	return ark.ArkType + "/" + ark.Prompt
}
