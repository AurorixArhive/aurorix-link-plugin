package parse

import "testing"

const douyinVideoPage = `<html><script>window._ROUTER_DATA = {"loaderData":{"video_(id)/page":{"videoInfoRes":{"item_list":[` +
	`{"desc":"猫在跳舞 #萌宠","author":{"nickname":"某某"},"video":{"play_addr":{"url_list":["https://aweme.snssdk.com/aweme/v1/playwm/?video_id=v0300"]},"cover":{"url_list":["https://p3-sign.douyinpic.com/cover.jpeg"]},"duration":15200}}` +
	`]}}}}</script></html>`

const douyinAlbumPage = `<html><script>window._ROUTER_DATA = {"loaderData":{"note_(id)/page":{"videoInfoRes":{"item_list":[` +
	`{"desc":"三张图记一下","author":{"nickname":"阿蓝"},"images":[{"url_list":["https://p3.douyinpic.com/1.jpeg"]},{"url_list":["https://p3.douyinpic.com/2.jpeg"]},{"url_list":["https://p3.douyinpic.com/3.jpeg"]}]}` +
	`]}}}}</script></html>`

func TestDouyinVideo(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.iesdouyin.com/share/video/7321234567890123456/": douyinVideoPage,
	})
	got, err := newDouyin(client).Parse("https://www.douyin.com/video/7321234567890123456")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Title != "猫在跳舞 #萌宠" || got.Author != "某某" {
		t.Fatalf("标题或作者不对: %+v", got)
	}
	if got.Duration != 15 {
		t.Fatalf("时长 %d, want 15 秒", got.Duration)
	}
	if got.Video == nil {
		t.Fatal("没取到视频")
	}
	if got.Video.URL != "https://aweme.snssdk.com/aweme/v1/play/?video_id=v0300" {
		t.Fatalf("去水印没生效: %s", got.Video.URL)
	}
	if got.Cover == "" {
		t.Fatal("没取到封面")
	}
	if got.PageURL != "https://www.douyin.com/video/7321234567890123456" {
		t.Fatalf("页面地址 %s", got.PageURL)
	}
}

func TestDouyinShortLink(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.iesdouyin.com/share/video/7321234567890123456/": douyinVideoPage,
	}).withRedirect("https://v.douyin.com/iRNBho6/", "https://www.iesdouyin.com/share/video/7321234567890123456/")
	got, err := newDouyin(client).Parse("https://v.douyin.com/iRNBho6/")
	if err != nil {
		t.Fatalf("短链解析失败: %v", err)
	}
	if got.Video == nil {
		t.Fatal("短链解析没取到视频")
	}
}

func TestDouyinAlbum(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.iesdouyin.com/share/video/7331234567890123456/": douyinAlbumPage,
	})
	got, err := newDouyin(client).Parse("https://www.douyin.com/video/7331234567890123456")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(got.Images) != 3 {
		t.Fatalf("图集数量 %d, want 3", len(got.Images))
	}
	if got.Images[0] != "https://p3.douyinpic.com/1.jpeg" || got.Images[2] != "https://p3.douyinpic.com/3.jpeg" {
		t.Fatalf("图片顺序不对: %v", got.Images)
	}
	if got.Kind() != "album" {
		t.Fatalf("应识别为图集, 得到 %s", got.Kind())
	}
}

// 页面里没有作品数据时要给出能看懂的错, 而不是空结果。
func TestDouyinPageWithoutData(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.iesdouyin.com/share/video/7321234567890123456/": "<html>没有数据的页面</html>",
	})
	if _, err := newDouyin(client).Parse("https://www.douyin.com/video/7321234567890123456"); err == nil {
		t.Fatal("没有作品数据时应报错")
	}
}
