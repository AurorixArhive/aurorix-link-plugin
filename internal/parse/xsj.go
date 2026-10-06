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
	xsjCodePattern  = regexp.MustCompile(`s\.xsj\.qq\.com/([A-Za-z0-9_-]+)`)
	xsjVideoPattern = regexp.MustCompile(`https?://[^"'\\ ]+\.(?:mp4|m3u8)[^"'\\ ]*`)
)

type xsj struct {
	env *env
}

func newXsj(env *env) *xsj { return &xsj{env: env} }

func (x *xsj) Parse(rawURL string) (model.Media, error) {
	resp, err := x.env.Get(rawURL, x.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("QQ小世界页面打不开: %w", err)
	}
	body := resp.Text()
	page := dict(xsjState(body))
	if page == nil {
		page = xsjFromHTML(body)
	}
	if page == nil {
		return model.Media{}, errors.New("QQ小世界页面里没找到作品数据(可能已删除)")
	}
	return xsjMedia(page, resp.URL), nil
}

func (x *xsj) headers() map[string]string {
	return map[string]string{
		"User-Agent": MobileUA,
		"Referer":    "https://s.xsj.qq.com/",
	}
}

// xsjState 页面把首屏数据挂在 window 变量下, 键名随版本变, 所以逐个标记试。
func xsjState(body string) any {
	for _, marker := range []string{"__INITIAL_STATE__", "window.__DATA__", "window.__INITIAL_DATA__"} {
		raw, ok := jsonAfter(body, marker)
		if !ok {
			continue
		}
		var root any
		if json.Unmarshal(raw, &root) == nil {
			return root
		}
	}
	return nil
}

// xsjFromHTML 没有结构化数据时退到最朴素的办法: 从页面里扫视频与图片地址。
func xsjFromHTML(body string) map[string]any {
	url := xsjVideoPattern.FindString(body)
	if url == "" {
		return nil
	}
	return map[string]any{"video": url, "title": xsjTitle(body)}
}

func xsjTitle(body string) string {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`<title[^>]*>(.*?)</title>`),
		regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`),
	} {
		if m := re.FindStringSubmatch(body); m != nil {
			if text := strings.TrimSpace(plainText(m[1])); text != "" {
				return text
			}
		}
	}
	return ""
}

func xsjMedia(page map[string]any, pageURL string) model.Media {
	media := model.Media{
		Platform: "QQ小世界",
		Title:    firstLine(nonEmpty(str(page["title"]), str(page["desc"]), firstString(page, "content"))),
		Author:   firstString(dict(page["author"]), "nick", "nickname", "name"),
		Desc:     firstLine(nonEmpty(str(page["desc"]), str(page["content"]))),
		Cover:    firstHTTP(page["cover"], page["poster"], page["thumb"]),
		PageURL:  pageURL,
	}
	if media.Title == "" {
		media.Title = "QQ小世界"
	}
	if url := firstHTTP(page["video"], page["videoUrl"], page["play_url"]); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": MobileUA, "Referer": "https://s.xsj.qq.com/"},
		}
		return media
	}
	for _, item := range list(page["images"]) {
		if url := firstHTTP(item); url != "" {
			media.Images = append(media.Images, url)
		}
	}
	return media
}
