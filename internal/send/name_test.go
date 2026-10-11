package send

import (
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
)

// 文件名不能从整条 URL 抠, 也不能一律写 .mp3 —— QQ 走 URL 直传时就是这么干的,
// 群里会收到 "....m4a_guid=...&vkey=..." 这种连后缀都被吃掉的名字。
func TestAudioNameTakesExtensionFromTheURL(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://x/a.m4a?guid=1452621576&vkey=31DF&uin=&redirect=1", "标题.m4a"},
		{"https://x/a.mp3", "标题.mp3"},
		{"https://x/a.flac?x=1", "标题.flac"},
		{"https://x/a.aac", "标题.aac"},
		{"https://x/a", "标题.mp3"},
	}
	for _, c := range cases {
		got := audioName(model.Audio{URL: c.url, Title: "标题"}, &model.Result{})
		if got != c.want {
			t.Errorf("audioName(%q) = %q, want %q", c.url, got, c.want)
		}
		for _, leak := range []string{"guid=", "vkey=", "http", "?"} {
			if strings.Contains(got, leak) {
				t.Errorf("文件名里漏了 URL 片段 %q: %q", leak, got)
			}
		}
	}
}

func TestAudioNameFallsBackAndSanitizes(t *testing.T) {
	got := audioName(model.Audio{URL: "https://x/a.mp3", Title: "A/B:C*D"}, &model.Result{})
	if got != "A_B_C_D.mp3" {
		t.Errorf("非法字符没换掉: %q", got)
	}
	got = audioName(model.Audio{URL: "https://x/a.mp3"}, &model.Result{Title: "外层的标题"})
	if got != "外层的标题.mp3" {
		t.Errorf("没歌名时该用外层标题: %q", got)
	}
	got = audioName(model.Audio{URL: "https://x/a.mp3"}, &model.Result{})
	if got != "audio.mp3" {
		t.Errorf("都没名字时该有兜底: %q", got)
	}
}
