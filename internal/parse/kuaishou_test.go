package parse

import "testing"

const kuaishouPage = `<html><script>window.INIT_STATE = {"fw-1":{"photo":{` +
	`"caption":"今晚的月亮真圆","userName":"阿张","coverUrl":"https://p1.a.yximgs.com/cover.jpg","duration":12400,` +
	`"mainMvUrls":[{"url":"https://txmov2.a.yximgs.com/video.mp4"}]}}}</script></html>`

const kuaishouAtlasPage = `<html><script>window.INIT_STATE = {"fw-2":{"photo":{` +
	`"caption":"四张图","userName":"小周","duration":0,` +
	`"atlas":{"cdn":"tx2.a.kwimgs.com","list":[{"path":"1.jpg"},{"path":"2.jpg"}]}}}};</script></html>`

func TestKuaishouVideo(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.kuaishou.com/short-video/abc": kuaishouPage,
	}).withRedirect("https://v.kuaishou.com/abc123", "https://www.kuaishou.com/short-video/abc")

	got, err := newKuaishou(client).Parse("https://v.kuaishou.com/abc123")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Title != "今晚的月亮真圆" || got.Author != "阿张" {
		t.Fatalf("标题或作者不对: %+v", got)
	}
	if got.Duration != 12 {
		t.Fatalf("时长 %d, want 12 秒", got.Duration)
	}
	if got.Video == nil || got.Video.URL != "https://txmov2.a.yximgs.com/video.mp4" {
		t.Fatalf("视频地址不对: %+v", got.Video)
	}
}

func TestKuaishouAtlas(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.kuaishou.com/short-video/xyz": kuaishouAtlasPage,
	})
	got, err := newKuaishou(client).Parse("https://www.kuaishou.com/short-video/xyz")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Kind() != "album" || len(got.Images) != 2 {
		t.Fatalf("应识别为图集: %+v", got)
	}
}

func TestKuaishouWithoutData(t *testing.T) {
	client := newFake(map[string]string{"https://www.kuaishou.com/short-video/empty": "<html></html>"})
	if _, err := newKuaishou(client).Parse("https://www.kuaishou.com/short-video/empty"); err == nil {
		t.Fatal("页面没有数据时应报错")
	}
}
