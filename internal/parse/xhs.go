package parse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"video-plugin/internal/model"
)

type xhs struct {
	client Client
}

func newXhs(client Client) *xhs { return &xhs{client: client} }

func (x *xhs) Parse(rawURL string) (model.Media, error) {
	// 移动端 UA 才拿得到笔记页数据; xhslink 短链用桌面 UA 会被踢到 404。
	resp, err := x.client.Get(rawURL, x.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("小红书页面打不开: %w", err)
	}
	note, err := xhsNote(resp.Text())
	if err != nil {
		return model.Media{}, err
	}
	return xhsMedia(note, resp.URL), nil
}

func (x *xhs) headers() map[string]string {
	return map[string]string{
		"User-Agent": MobileUA,
		"Referer":    "https://www.xiaohongshu.com/",
	}
}

func xhsNote(html string) (map[string]any, error) {
	raw, ok := jsonAfter(html, "window.__INITIAL_STATE__")
	if !ok {
		return nil, errors.New("小红书页面里没找到笔记数据(可能需要重新复制分享链接)")
	}
	// 小红书页面里写的是 JS 的 undefined, 不是合法 JSON
	raw = bytes.ReplaceAll(raw, []byte("undefined"), []byte("null"))
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, errors.New("小红书笔记数据解析失败")
	}
	note := findMap(root, []string{"noteId"}, []string{"imageList", "video", "desc"})
	if note == nil {
		return nil, errors.New("小红书笔记取不到(可能已删除、私密, 或分享链接里的 token 过期)")
	}
	return note, nil
}

func xhsMedia(note map[string]any, pageURL string) model.Media {
	noteID := str(note["noteId"])
	media := model.Media{
		Platform: "小红书",
		Title:    firstLine(nonEmpty(str(note["title"]), str(note["desc"]))),
		Author:   firstString(dict(note["user"]), "nickname", "nickName"),
		Desc:     firstLine(str(note["desc"])),
		PageURL:  nonEmpty("https://www.xiaohongshu.com/explore/"+noteID, pageURL),
	}
	if noteID == "" {
		media.PageURL = pageURL
	}
	if media.Title == "" {
		media.Title = "小红书笔记"
	}

	if images := xhsImages(note); len(images) > 0 {
		media.Images = images
		media.Cover = images[0]
	}
	if url := xhsVideoURL(note); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": MobileUA, "Referer": "https://www.xiaohongshu.com/"},
		}
	}
	return media
}

func xhsImages(note map[string]any) []string {
	items := list(note["imageList"])
	out := make([]string, 0, len(items))
	for _, item := range items {
		entry := dict(item)
		url := firstHTTP(entry["urlDefault"], entry["urlPre"], entry["url"])
		if url != "" {
			out = append(out, url)
		}
	}
	return out
}

func xhsVideoURL(note map[string]any) string {
	stream := dict(dict(dict(note["video"])["media"])["stream"])
	if stream == nil {
		return ""
	}
	// h264 是兼容性最好的那一路, 优先它
	for _, codec := range []string{"h264", "h265", "av1"} {
		if url := firstHTTP(stream[codec]); url != "" {
			return url
		}
	}
	return ""
}
