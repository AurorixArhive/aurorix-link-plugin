package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"video-plugin/internal/model"
)

var ixiguaIDPattern = regexp.MustCompile(`ixigua\.com/(?:video/)?(\d{10,})`)

// 西瓜与抖音同源, 一样要老安卓 UA 才拿得到 SSR 数据。

type ixigua struct {
	env *env
}

func newIxigua(env *env) *ixigua { return &ixigua{env: env} }

func (x *ixigua) Parse(rawURL string) (model.Media, error) {
	id, err := x.identify(rawURL)
	if err != nil {
		return model.Media{}, err
	}
	resp, err := x.env.Get("https://www.ixigua.com/"+id, x.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("西瓜页面打不开: %w", err)
	}
	item, err := ixiguaItem(resp.Text())
	if err != nil {
		return model.Media{}, err
	}
	return ixiguaMedia(item, id), nil
}

func (x *ixigua) identify(rawURL string) (string, error) {
	if m := ixiguaIDPattern.FindStringSubmatch(rawURL); m != nil {
		return m[1], nil
	}
	// v.ixigua.com 短链要跟随跳转
	resp, err := x.env.Get(rawURL, x.headers())
	if err != nil {
		return "", fmt.Errorf("短链展开失败: %w", err)
	}
	if m := ixiguaIDPattern.FindStringSubmatch(resp.URL); m != nil {
		return m[1], nil
	}
	return "", errors.New("没从这个链接里认出西瓜作品号")
}

func (x *ixigua) headers() map[string]string {
	return map[string]string{
		"User-Agent": byteDanceUA,
		"Referer":    "https://www.ixigua.com/",
	}
}

// ixiguaItem 与抖音同源, 作品数据同样在 SSR 页面的 _ROUTER_DATA 里。
func ixiguaItem(html string) (map[string]any, error) {
	raw, ok := jsonAfter(html, "_ROUTER_DATA")
	if !ok {
		return nil, errors.New("西瓜页面里没找到作品数据(可能已删除)")
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return nil, errors.New("西瓜作品数据解析失败")
	}
	item := findMap(root, []string{"video"}, []string{"title", "abstract", "play_addr"})
	if item == nil {
		return nil, errors.New("西瓜没给作品详情")
	}
	return item, nil
}

func ixiguaMedia(item map[string]any, id string) model.Media {
	video := dict(item["video"])
	media := model.Media{
		Platform: "西瓜视频",
		Title:    firstLine(nonEmpty(str(item["title"]), str(item["abstract"]))),
		Author:   firstString(item, "source", "user_name", "author"),
		Desc:     firstLine(nonEmpty(str(item["abstract"]), str(item["description"]))),
		Cover:    firstHTTP(video["cover"], item["cover"], item["poster"]),
		Duration: number(video["duration"]) / 1000,
		PageURL:  "https://www.ixigua.com/" + id,
	}
	if media.Title == "" {
		media.Title = "西瓜视频"
	}
	if url := ixiguaPlayURL(video); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": byteDanceUA, "Referer": "https://www.ixigua.com/"},
		}
	}
	return media
}

func ixiguaPlayURL(video map[string]any) string {
	raw := firstHTTP(video["play_addr"], video["play_addr_h264"], video["download_addr"])
	// 分享页给的是带水印地址, 换成 play 拿原片
	return replacePlaywm(raw)
}
