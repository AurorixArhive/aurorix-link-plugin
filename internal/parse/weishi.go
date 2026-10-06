package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"video-plugin/internal/model"
)

var weishiIDPattern = regexp.MustCompile(`/([A-Za-z0-9]{6,})`)

type weishi struct {
	env *env
}

func newWeishi(env *env) *weishi { return &weishi{env: env} }

func (w *weishi) Parse(rawURL string) (model.Media, error) {
	id := weishiID(rawURL)
	if id == "" {
		return model.Media{}, errors.New("没从这个链接里认出微视作品号")
	}
	// 分享页只是壳, 数据在这个 h5 接口里
	target := "https://h5.weishi.qq.com/webapp/json/weishi/WSH5GetPlayPage?feedid=" + id
	resp, err := w.env.Get(target, w.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("微视接口打不开: %w", err)
	}
	var body struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Feed map[string]any `json:"feed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return model.Media{}, errors.New("微视返回的数据看不懂")
	}
	if body.Data.Feed == nil {
		return model.Media{}, fmt.Errorf("微视没给作品数据: %s", nonEmpty(body.Msg, "可能已删除或需要登录"))
	}
	return weishiMedia(body.Data.Feed, id), nil
}

func (w *weishi) headers() map[string]string {
	head := map[string]string{
		"User-Agent": MobileUA,
		"Referer":    "https://video.weishi.qq.com/",
		"Accept":     "application/json, text/plain, */*",
	}
	if cookie := w.env.cookie("weishi"); cookie != "" {
		head["Cookie"] = cookie
	}
	return head
}

func weishiID(rawURL string) string {
	m := weishiIDPattern.FindStringSubmatch(rawURL)
	if m == nil {
		return ""
	}
	return m[1]
}

func weishiMedia(feed map[string]any, id string) model.Media {
	media := model.Media{
		Platform: "微视",
		Title:    firstLine(nonEmpty(str(feed["feed_desc"]), str(feed["desc"]), str(feed["title"]))),
		Author:   firstString(dict(feed["poster"]), "nick", "nickname", "name"),
		Desc:     firstLine(nonEmpty(str(feed["feed_desc"]), str(feed["desc"]))),
		PageURL:  "https://video.weishi.qq.com/" + id,
	}
	if media.Title == "" {
		media.Title = "微视视频"
	}
	media.Cover = firstHTTP(feed["cover"], feed["video_cover"], feed["cover_url"])
	media.Duration = number(feed["video_duration"]) / 1000

	if url := weishiPlayURL(feed); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": MobileUA, "Referer": "https://video.weishi.qq.com/"},
		}
	}
	return media
}

// weishiPlayURL 地址藏在多层 video 对象里, 直接从整块里扫 http 地址比写死路径稳。
func weishiPlayURL(feed map[string]any) string {
	video := dict(feed["video"])
	if url := firstHTTP(video["url"], video["video_url"], video["play_url"], feed["video_url"], feed["play_url"]); url != "" {
		return url
	}
	return ""
}
