package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"video-plugin/internal/model"
)

type kuaishou struct {
	client Client
}

func newKuaishou(client Client) *kuaishou { return &kuaishou{client: client} }

func (k *kuaishou) Parse(rawURL string) (model.Media, error) {
	// 短链要跟随跳转, 落地页里才有 INIT_STATE。
	resp, err := k.client.Get(rawURL, k.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("快手页面打不开: %w", err)
	}
	photo, err := kuaishouPhoto(resp.Text())
	if err != nil {
		return model.Media{}, err
	}
	return kuaishouMedia(photo, resp.URL), nil
}

func (k *kuaishou) headers() map[string]string {
	return map[string]string{
		"User-Agent": MobileUA,
		"Referer":    "https://v.kuaishou.com/",
	}
}

// kuaishouPhoto 从页面里挖作品对象。快手在 INIT_STATE 与 APOLLO_STATE 之间来回换,
// 两个都试, 按"有 caption 又有播放地址"的特征定位。
func kuaishouPhoto(html string) (map[string]any, error) {
	for _, marker := range []string{"window.INIT_STATE", "window.__APOLLO_STATE__", "window.__INITIAL_STATE__"} {
		raw, ok := jsonAfter(html, marker)
		if !ok {
			continue
		}
		var root any
		if err := json.Unmarshal(raw, &root); err != nil {
			continue
		}
		if photo := findMap(root, []string{"caption"}, []string{"photoUrl", "mainMvUrls", "srcNoMark", "atlas"}); photo != nil {
			return photo, nil
		}
	}
	return nil, errors.New("快手页面里没找到作品数据(可能已删除或需要登录)")
}

func kuaishouMedia(photo map[string]any, pageURL string) model.Media {
	media := model.Media{
		Platform: "快手",
		Title:    firstLine(str(photo["caption"])),
		Author:   firstString(photo, "userName", "nickName", "name"),
		Cover:    firstHTTP(photo["coverUrl"], photo["coverUrls"], photo["webpCoverUrl"]),
		Duration: number(photo["duration"]) / 1000,
		PageURL:  pageURL,
	}
	if media.Title == "" {
		media.Title = "快手作品"
	}

	if images := kuaishouImages(photo); len(images) > 0 {
		media.Images = images
		return media
	}
	if url := firstHTTP(photo["mainMvUrls"], photo["photoUrl"], photo["srcNoMark"]); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": MobileUA, "Referer": "https://v.kuaishou.com/"},
		}
	}
	return media
}

// kuaishouImages 取图集: 快手给的是相对路径, 要用图集自带的 CDN 域名拼完整地址。
func kuaishouImages(photo map[string]any) []string {
	atlas := dict(photo["atlas"])
	if atlas == nil {
		return nil
	}
	host := firstString(atlas, "cdn", "host", "domain")
	out := make([]string, 0, 4)
	for _, item := range list(atlas["list"]) {
		entry := dict(item)
		if url := firstHTTP(entry); url != "" {
			out = append(out, url)
			continue
		}
		if path := firstString(entry, "path", "route", "url"); path != "" && host != "" {
			out = append(out, "https://"+host+"/"+strings.TrimPrefix(path, "/"))
		}
	}
	return out
}
