package rednote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

// 页面里带 JS 的 undefined, 不是合法 JSON —— 这条盯着那个容错。
const videoPage = `<html><script>window.__INITIAL_STATE__={"note":{"noteDetailMap":{` +
	`"66f0a1b2c3d4e5f60718293a":{"note":{"noteId":"66f0a1b2c3d4e5f60718293a","title":"海边的一天","desc":"随手拍的",` +
	`"user":{"nickname":"阿海"},"video":{"media":{"stream":{"h264":[{"masterUrl":"https://sns-video.xhscdn.com/v.mp4"}]}}}` +
	`,"imageList":[{"urlDefault":"https://sns-img.xhscdn.com/cover.jpg"}]}}}}},` +
	`"user":{"loggedIn":undefined}}</script></html>`

const imagePage = `<html><script>window.__INITIAL_STATE__={"note":{"noteDetailMap":{` +
	`"65f0a1b2c3d4e5f60718293a":{"note":{"noteId":"65f0a1b2c3d4e5f60718293a","title":"周末去哪","desc":"去了海边\n风很大",` +
	`"user":{"nickname":"小美"},"imageList":[{"urlDefault":"https://sns-img.xhscdn.com/1.jpg"},{"urlPre":"https://sns-img.xhscdn.com/2.jpg"}]}}}}}</script></html>`

// 短链那一跳必须用 GET: 旧实现直接用 HEAD 问, 小红书只回一张空壳, 于是永远"没跳转"。
func TestShortLinkFollowsWithGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/s/abc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/explore/66f0a1b2c3d4e5f60718293a", http.StatusFound)
	})
	mux.HandleFunc("/explore/66f0a1b2c3d4e5f60718293a", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(videoPage))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &Parser{client: netx.New(20, headers)}
	got, err := p.shortLink(context.Background(), &parse.Match{URL: server.URL + "/s/abc"})
	if err != nil {
		t.Fatalf("短链解析失败: %v", err)
	}
	videos := got.Videos()
	if len(videos) != 1 || videos[0].URL != "https://sns-video.xhscdn.com/v.mp4" {
		t.Fatalf("视频地址不对: %+v", videos)
	}
	if got.Title != "海边的一天" || got.Author.Name != "阿海" {
		t.Errorf("标题或作者不对: %+v", got)
	}
}

func TestNoteWithImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(imagePage))
	}))
	defer server.Close()

	old := noteBase
	noteBase = server.URL
	defer func() { noteBase = old }()

	p := &Parser{client: netx.New(20, headers)}
	got, err := p.note(context.Background(), &parse.Match{
		URL:    "explore/65f0a1b2c3d4e5f60718293a",
		Params: map[string]string{"note_id": "65f0a1b2c3d4e5f60718293a"},
	})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Title != "周末去哪" || got.Author.Name != "小美" {
		t.Fatalf("标题或作者不对: %+v", got)
	}
	images := got.Images()
	if len(images) != 2 || images[0] != "https://sns-img.xhscdn.com/1.jpg" {
		t.Fatalf("图片不对: %v", images)
	}
	if len(got.Videos()) != 0 {
		t.Errorf("图集不该有视频: %+v", got.Videos())
	}
}

// 登录墙: 页面里没有笔记数据时要报错, 不能静默返回一张空卡片。
func TestNoteWithoutStateFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>请登录</body></html>`))
	}))
	defer server.Close()

	p := &Parser{client: netx.New(20, headers)}
	if _, err := p.shortLink(context.Background(), &parse.Match{URL: server.URL + "/s/abc"}); err == nil {
		t.Fatal("没有笔记数据时该报错")
	}
}

// undefined 被换成 null 后仍然找得到笔记对象, 换的是路径不是内容。
func TestNoteToleratesUndefined(t *testing.T) {
	note, err := noteOf(videoPage)
	if err != nil {
		t.Fatalf("容错没生效: %v", err)
	}
	if str(note["title"]) != "海边的一天" {
		t.Errorf("取到的不是笔记对象: %+v", note)
	}
}
