package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"video-plugin/internal/model"
)

var (
	weiboIDInPath = regexp.MustCompile(`/(?:detail|status)/([A-Za-z0-9]+)`)
	weiboTagRE    = regexp.MustCompile(`<[^>]*>`)
)

type weibo struct {
	env *env
}

func newWeibo(env *env) *weibo { return &weibo{env: env} }

func (w *weibo) Parse(rawURL string) (model.Media, error) {
	id := weiboID(rawURL)
	if id == "" {
		return model.Media{}, errors.New("没从这个链接里认出微博作品号")
	}
	// 这个接口只吃数字或 base62 短码, PC 长链要先换算, 所以先短链展开一次
	id = w.expand(rawURL, id)

	resp, err := w.env.Get("https://m.weibo.cn/statuses/show?id="+id, w.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("微博接口打不开: %w", err)
	}
	var body map[string]any
	if json.Unmarshal(resp.Body, &body) != nil {
		return model.Media{}, errors.New("微博返回的数据看不懂")
	}
	if len(body) == 0 || str(body["id"]) == "" && str(body["text"]) == "" {
		return model.Media{}, errors.New("微博取不到这条内容(可能已删除或需要登录)")
	}
	return weiboMedia(body, id), nil
}

func (w *weibo) headers() map[string]string {
	head := map[string]string{
		"User-Agent":       MobileUA,
		"Referer":          "https://m.weibo.cn/",
		"X-Requested-With": "XMLHttpRequest",
		"Accept":           "application/json, text/plain, */*",
	}
	if cookie := w.env.cookie("weibo"); cookie != "" {
		head["Cookie"] = cookie
	}
	return head
}

// expand 把 weibo.com/用户/短码 这种长链还原成接口要的数字 id。
func (w *weibo) expand(rawURL, id string) string {
	if !strings.Contains(rawURL, "weibo.com") || strings.Contains(rawURL, "m.weibo.cn") {
		return id
	}
	resp, err := w.env.Get(rawURL, w.headers())
	if err != nil {
		return id
	}
	if m := weiboIDInPath.FindStringSubmatch(resp.URL); m != nil {
		return m[1]
	}
	return id
}

func weiboID(rawURL string) string {
	if m := weiboIDInPath.FindStringSubmatch(rawURL); m != nil {
		return m[1]
	}
	return ""
}

func weiboMedia(body map[string]any, id string) model.Media {
	page := dict(body["page_info"])
	media := model.Media{
		Platform: "微博",
		Title:    firstLine(weiboText(body)),
		Author:   firstString(dict(body["user"]), "screen_name", "name"),
		Desc:     firstLine(weiboText(body)),
		PageURL:  "https://m.weibo.cn/detail/" + id,
	}
	if media.Title == "" {
		media.Title = "微博"
	}
	media.Cover = firstHTTP(page["page_pic"], page["cover"], body["thumbnail_pic"])

	if pics := weiboPics(body); len(pics) > 0 {
		media.Images = pics
		if media.Cover == "" {
			media.Cover = pics[0]
		}
	}

	// page_info 里带视频时优先给视频, 不然纯图博也会被当视频处理
	if url := weiboPlayURL(page); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": MobileUA, "Referer": "https://weibo.com/"},
		}
		media.Images = nil
	}
	return media
}

// weiboPlayURL 微博的地址一路一个键名, 从高到低试, 取到即用。
func weiboPlayURL(page map[string]any) string {
	if page == nil {
		return ""
	}
	urls := dict(page["urls"])
	if url := firstHTTP(urls["mp4_720p_mp4"], urls["mp4_hd_mp4"], urls["mp4_ld_mp4"]); url != "" {
		return url
	}
	info := dict(page["media_info"])
	return firstHTTP(info["stream_url_hd"], info["stream_url"], page["media_url"])
}

func weiboPics(body map[string]any) []string {
	list := list(body["pics"])
	out := make([]string, 0, len(list))
	for _, item := range list {
		if url := firstHTTP(dict(item)["large"], dict(item)["url"]); url != "" {
			out = append(out, url)
		}
	}
	return out
}

// weiboText 微博正文是 HTML 片段, 去掉标签再折叠空白。
func weiboText(body map[string]any) string {
	raw := nonEmpty(str(body["longText"]), str(body["longTextContent"]), str(body["text"]))
	return strings.Join(strings.Fields(weiboTagRE.ReplaceAllString(raw, " ")), " ")
}
