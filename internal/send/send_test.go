package send

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/event"
	"github.com/KasumiYuku/Aurorix/lib/message"
	"github.com/KasumiYuku/Aurorix/lib/structers"

	"video-plugin/internal/model"
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

func TestReasonKeepsReadableText(t *testing.T) {
	if got := reason(errors.New("抖音没给作品详情(可能已删除或仅自己可见)")); got == "" || strings.Contains(got, "稍后再试") {
		t.Fatalf("能看懂的话应原样透出, 得到 %q", got)
	}
	for _, err := range []error{
		errors.New("request failed: Get \"https://api.bilibili.com/x\": dial tcp: timeout"),
		errors.New(strings.Repeat("很长的错误", 40)),
		nil,
	} {
		if got := reason(err); got != "解析没成功，稍后再试" {
			t.Fatalf("应换成不透明文案, 得到 %q", got)
		}
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

func TestMetaLine(t *testing.T) {
	cases := []struct {
		media model.Media
		want  string
	}{
		{model.Media{}, "点标题去原页面看"},
		{model.Media{Author: "某某", Duration: 95, Stat: "播放 1.2万"}, "某某 · 01:35 · 播放 1.2万"},
		{model.Media{Author: "小美", Images: []string{"a", "b", "c"}}, "小美 · 3 张图"},
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

// 普通聊天不该留痕: 群里每句话都打一条日志, 日志里就什么都看不出来了。
func TestWorthTracingOnlyOnCardOrForward(t *testing.T) {
	if worthTracing(&event.MessageEvent{Content: "今天天气不错"}) {
		t.Fatal("有正文的纯文本不该留痕")
	}
	if worthTracing(&event.MessageEvent{Content: "看图", Attachments: []message.Attachment{{ContentType: "image/png"}}}) {
		t.Fatal("带图的普通消息不该留痕")
	}

	cases := map[string]*event.MessageEvent{
		"卡片":  {MsgType: 3, Ark: &structers.ArkData{ArkType: "miniapp"}},
		"转发":  {MsgType: 102, Elements: []structers.MsgElement{{Content: "被转发的内容"}}},
		"富媒体": {MsgType: 7},
		"空消息": {},
	}
	for name, ev := range cases {
		if !worthTracing(ev) {
			t.Fatalf("%s 应该留痕: %+v", name, ev)
		}
	}
}
