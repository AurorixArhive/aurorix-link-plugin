package parse

import "testing"

// 小红书页面里带 JS 的 undefined, 不是合法 JSON —— 这一条专门盯着那个容错。
const xhsNotePage = `<html><script>window.__INITIAL_STATE__={"note":{"noteDetailMap":{` +
	`"65f0a1b2c3d4e5f60718293a":{"note":{"noteId":"65f0a1b2c3d4e5f60718293a","title":"周末去哪","desc":"去了海边\n风很大",` +
	`"type":"normal","user":{"nickname":"小美"},"imageList":[{"urlDefault":"https://sns-img.xhscdn.com/1.jpg"},{"urlDefault":"https://sns-img.xhscdn.com/2.jpg"}]}}}},` +
	`"user":{"loggedIn":undefined}}</script></html>`

const xhsVideoPage = `<html><script>window.__INITIAL_STATE__={"note":{"noteDetailMap":{` +
	`"66f0a1b2c3d4e5f60718293a":{"note":{"noteId":"66f0a1b2c3d4e5f60718293a","title":"海边的一天","desc":"随手拍的",` +
	`"user":{"nickname":"阿海"},"video":{"media":{"stream":{"h264":[{"masterUrl":"https://sns-video.xhscdn.com/v.mp4"}]}}}}}}}}}</script></html>`

func TestXhsNoteWithImages(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.xiaohongshu.com/explore/65f0a1b2c3d4e5f60718293a?xsec_token=ABtoken": xhsNotePage,
	})
	got, err := newXhs(testEnv(client, nil)).Parse("https://www.xiaohongshu.com/explore/65f0a1b2c3d4e5f60718293a?xsec_token=ABtoken")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Title != "周末去哪" || got.Author != "小美" {
		t.Fatalf("标题或作者不对: %+v", got)
	}
	if got.Desc != "去了海边" {
		t.Fatalf("文案应只取第一行: %q", got.Desc)
	}
	if len(got.Images) != 2 || got.Images[0] != "https://sns-img.xhscdn.com/1.jpg" {
		t.Fatalf("图片不对: %v", got.Images)
	}
	if got.Kind() != "album" {
		t.Fatalf("应识别为图集, 得到 %s", got.Kind())
	}
}

func TestXhsVideoNote(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.xiaohongshu.com/explore/66f0a1b2c3d4e5f60718293a": xhsVideoPage,
	})
	got, err := newXhs(testEnv(client, nil)).Parse("https://www.xiaohongshu.com/explore/66f0a1b2c3d4e5f60718293a")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Video == nil || got.Video.URL != "https://sns-video.xhscdn.com/v.mp4" {
		t.Fatalf("视频地址不对: %+v", got.Video)
	}
	if got.Kind() != "video" {
		t.Fatalf("应识别为视频, 得到 %s", got.Kind())
	}
}

// 登录墙: 页面里没有笔记数据时要回一句用户能照做的话。
func TestXhsLoginWall(t *testing.T) {
	client := newFake(map[string]string{
		"https://www.xiaohongshu.com/explore/67f0a1b2c3d4e5f60718293a": "<html><body>请登录</body></html>",
	})
	_, err := newXhs(testEnv(client, nil)).Parse("https://www.xiaohongshu.com/explore/67f0a1b2c3d4e5f60718293a")
	if err == nil {
		t.Fatal("没有笔记数据时应报错")
	}
}
