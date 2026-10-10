package send

import (
	"strings"
	"testing"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/event"
	"github.com/KasumiYuku/Aurorix/lib/message"
	"github.com/KasumiYuku/Aurorix/lib/structers"

	"github.com/AurorixArhive/aurorix-video-plugin/internal/model"
)

func TestClaimBlocksWithinTTL(t *testing.T) {
	p := New(nil, nil)
	if !p.claim("scope:g1", time.Minute) {
		t.Fatal("第一次应放行")
	}
	if p.claim("scope:g1", time.Minute) {
		t.Fatal("冷却期内不该再放行")
	}
	if !p.claim("scope:g2", time.Minute) {
		t.Fatal("换个群应放行")
	}
}

func TestClaimWithoutTTLAlwaysAllows(t *testing.T) {
	p := New(nil, nil)
	for i := 0; i < 3; i++ {
		if !p.claim("scope:g1", 0) {
			t.Fatal("冷却为 0 表示不限制, 应每次都放行")
		}
	}
}

func TestClaimExpiresAndCleans(t *testing.T) {
	p := New(nil, nil)
	p.recent["old"] = time.Now().Add(-time.Second)
	for i := 0; i < 1100; i++ {
		p.claim(string(rune('a'+i%26))+string(rune('a'+i/26)), time.Minute)
	}
	p.mu.Lock()
	_, stale := p.recent["old"]
	p.mu.Unlock()
	if stale {
		t.Fatal("表太大时应顺手清掉过期项")
	}
}

func TestVideoPlanSkipsLongVideo(t *testing.T) {
	p := New(nil, nil)
	got := model.Media{Duration: 1200, Video: &model.Video{URL: "https://x/v.mp4"}}
	note, path := p.videoPlan(got, Config{SendVideo: true, MaxDuration: 600, MaxSizeMB: 50})
	if path != "" {
		t.Fatal("超时长的视频不该下载")
	}
	if !strings.Contains(note, "超过") {
		t.Fatalf("应说明为什么没发本体: %q", note)
	}
}

func TestVideoPlanSkipsWhenVideoDisabled(t *testing.T) {
	p := New(nil, nil)
	got := model.Media{Duration: 10, Video: &model.Video{URL: "https://x/v.mp4"}}
	note, path := p.videoPlan(got, Config{SendVideo: false})
	if note != "" || path != "" {
		t.Fatalf("关掉视频本体时不该下载也不该提示: %q %q", note, path)
	}
}

// 作者不在这一行(它跟着标题走), 其余什么都不认识时返回空串 ——
// 那一段就不该出现在卡片上, 而不是拿一句废话占位。
func TestMetaLine(t *testing.T) {
	cases := []struct {
		media model.Media
		want  string
	}{
		{model.Media{}, ""},
		{model.Media{Title: "标题", Author: "某某"}, ""},
		{model.Media{Author: "某某", Duration: 95, Stat: "播放 1.2万"}, "01:35 · 播放 1.2万"},
		{model.Media{Author: "小美", Images: []string{"a", "b", "c"}}, "3 张图"},
		{model.Media{Duration: 14400, Stat: "播放 1 亿"}, "4:00:00 · 播放 1 亿"},
		{model.Media{Author: "小美", Images: []string{"a"}}, ""},
	}
	for _, c := range cases {
		if got := metaLine(c.media); got != c.want {
			t.Fatalf("metaLine = %q, want %q", got, c.want)
		}
	}
}

func TestEmojiFollowsKind(t *testing.T) {
	video := model.Media{Video: &model.Video{URL: "x"}}
	album := model.Media{Images: []string{"x"}}
	text := model.Media{}
	if emoji(video) != "🎬" || emoji(album) != "🖼" || emoji(text) != "📄" {
		t.Fatalf("图标不对: %s %s %s", emoji(video), emoji(album), emoji(text))
	}
}

func TestDurationText(t *testing.T) {
	cases := map[int]string{0: "", 59: "00:59", 95: "01:35", 3725: "1:02:05"}
	for seconds, want := range cases {
		if got := durationText(seconds); got != want {
			t.Fatalf("durationText(%d) = %q, want %q", seconds, got, want)
		}
	}
}

func TestSizeText(t *testing.T) {
	if got := sizeText(3 << 20); got != "3.0 MB" {
		t.Fatalf("sizeText = %q", got)
	}
	if got := sizeText(512 << 10); got != "512 KB" {
		t.Fatalf("sizeText = %q", got)
	}
}

// 解析失败必须把额度还回去: 一次网络抖动不该让这条链接在去重窗口里再也发不动,
// 而用户那边看到的只是"没反应"。
func TestReleaseReturnsQuota(t *testing.T) {
	p := New(nil, nil)
	if !p.claim("link:u", time.Minute) {
		t.Fatal("第一次应放行")
	}
	if p.claim("link:u", time.Minute) {
		t.Fatal("窗口内应拦住")
	}
	p.release("link:u")
	if !p.claim("link:u", time.Minute) {
		t.Fatal("还了额度就该能再试")
	}
}

func TestWindowText(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "30 秒",
		10 * time.Minute: "10 分钟",
		90 * time.Second: "1 分钟",
	}
	for d, want := range cases {
		if got := windowText(d); got != want {
			t.Fatalf("windowText(%v) = %q, want %q", d, got, want)
		}
	}
}

// 普通聊天不该留痕: 群里每句话都打一条日志, 日志里就什么都看不出来了。
func TestWorthTracingOnlyOnCardOrForward(t *testing.T) {
	if worthTracing(&event.MessageEvent{Content: "今天天气不错"}) {
		t.Fatal("有正文的纯文本不该留痕")
	}
	if worthTracing(&event.MessageEvent{Content: "看图", Attachments: []message.Attachment{{ContentType: "image/png"}}}) {
		t.Fatal("带图的普通消息不该留痕")
	}

	// 引用别人的消息回复是正常聊天: 元素里装的是被引用那句话, 不是平台推了怪东西。
	quoted := &event.MessageEvent{
		MsgType:  103,
		Content:  " <@40F71B126D2CF0836DC52171CAC57842> ？",
		Elements: []structers.MsgElement{{MessageType: 103, Content: "上一条消息"}},
	}
	if worthTracing(quoted) {
		t.Fatal("引用消息不该留痕")
	}

	cases := map[string]*event.MessageEvent{
		"卡片":    {MsgType: 3, Ark: &structers.ArkData{ArkType: "miniapp"}},
		"合并转发":  {MsgType: 102, Elements: []structers.MsgElement{{Content: "被转发的内容"}}},
		"转发的卡片": {MsgType: 103, Elements: []structers.MsgElement{{ArkData: &structers.ArkData{ArkType: "miniapp"}}}},
		"空消息":   {},
	}
	for name, ev := range cases {
		if !worthTracing(ev) {
			t.Fatalf("%s 应该留痕: %+v", name, ev)
		}
	}
}

// 直传栽过的平台要记住: 再让 QQ 去拉一条它拉不到的链, 白等十几秒之后还得再下一遍。
func TestDirectFailBlocksPlatform(t *testing.T) {
	p := New(nil, nil)
	if p.blocked(directFailKey("bilibili")) {
		t.Fatal("没栽过不该拦")
	}
	p.claim(directFailKey("bilibili"), time.Minute)
	if !p.blocked(directFailKey("bilibili")) {
		t.Fatal("栽过之后该拦")
	}
	if p.blocked(directFailKey("douyin")) {
		t.Fatal("不该连累别的平台")
	}
}

// 空文本必须真的消失 —— 模板引擎靠空数组判断"这段不出现",
// 塞个空串进去就会在卡片上留下一块空白引用, 或者标题后面挂一个孤零零的" · "。
func TestPresentSkipsEmpty(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\t"} {
		if got := present(text); len(got) != 0 {
			t.Fatalf("空白文本应产出空片段, 得到 %#v", got)
		}
	}
	got := present(" 索尼音乐中国 ")
	if len(got) != 1 {
		t.Fatalf("有内容应产出恰好一段, 得到 %#v", got)
	}
	if text, _ := got[0].(map[string]any)["text"].(string); text != "索尼音乐中国" {
		t.Fatalf("内容没去掉首尾空白: %#v", got)
	}
}

// 简介和标题一字不差时不再重复一遍: 那看着像排版故障, 不像信息。
func TestDescParagraphDropsTitleEcho(t *testing.T) {
	if got := descParagraph(model.Media{Title: "标题", Desc: "标题"}); len(got) != 0 {
		t.Fatalf("简介只是复述标题时不该出现: %#v", got)
	}
	if got := descParagraph(model.Media{Title: "标题", Desc: " 标题 "}); len(got) != 0 {
		t.Fatalf("首尾空白不该让它变成另一段: %#v", got)
	}
	if got := descParagraph(model.Media{Title: "标题", Desc: "别的"}); len(got) != 1 {
		t.Fatalf("有真简介时该出现: %#v", got)
	}
	if got := descParagraph(model.Media{Title: "标题"}); len(got) != 0 {
		t.Fatalf("没简介时不该出现: %#v", got)
	}
}
