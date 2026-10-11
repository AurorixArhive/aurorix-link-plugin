package send

import (
	"testing"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
)

// http 直链交给 QQ 之后接口回成功、群里却没有视频, 所以只放 https 过去。
func TestDirectUploadOnlyForHTTPS(t *testing.T) {
	if directUploadOK("douyin", "http://v.douyinvod.com/a.mp4") {
		t.Error("http 直链不该直传")
	}
	if !directUploadOK("douyin", "https://v.douyinvod.com/a.mp4") {
		t.Error("https 直链应该直传")
	}
	if directUploadOK("kuaishou", "https://txmov2.a.kwimgs.com/a.mp4") {
		t.Error("快手栽过一次, 不该再直传")
	}
}

// 框架对公网 URL 是直通给 QQ 的, http 会被它当成普通链接。
func TestHTTPSImageUpgradesScheme(t *testing.T) {
	if got := httpsImage("http://sns-webpic-qc.xhscdn.com/a.jpg"); got != "https://sns-webpic-qc.xhscdn.com/a.jpg" {
		t.Errorf("http 该抬成 https, 得到 %q", got)
	}
	if got := httpsImage("https://p3-pc-sign.douyinpic.com/a.jpeg"); got != "https://p3-pc-sign.douyinpic.com/a.jpeg" {
		t.Errorf("https 该原样保留, 得到 %q", got)
	}
}

// 视频帖没有图但有封面; 不挂上去标题下面就空着。
func TestCardImagesFallsBackToCover(t *testing.T) {
	videoOnly := &model.Result{
		Platform: model.Platform{Name: "douyin"},
		Content:  []model.Content{model.Video{URL: "https://x/a.mp4", Cover: "https://x/cover.jpg"}},
	}
	if got := cardImages(videoOnly); len(got) != 1 || got[0] != "https://x/cover.jpg" {
		t.Errorf("视频帖该用封面, 得到 %v", got)
	}

	album := &model.Result{Content: []model.Content{model.Image{URL: "https://x/1.jpg"}, model.Image{URL: "https://x/2.jpg"}}}
	if got := cardImages(album); len(got) != 2 {
		t.Errorf("图集该原样给出两张, 得到 %v", got)
	}

	if got := cardImages(&model.Result{}); len(got) != 0 {
		t.Errorf("什么都没有时该是空的, 得到 %v", got)
	}
}
