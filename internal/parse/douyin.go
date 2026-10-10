package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/AurorixArhive/aurorix-video-plugin/internal/model"
)

var douyinIDPattern = regexp.MustCompile(`/(?:video|note)/(\d+)`)

type douyin struct {
	env *env
}

func newDouyin(env *env) *douyin { return &douyin{env: env} }

func (d *douyin) Parse(rawURL string) (model.Media, error) {
	id, err := d.identify(rawURL)
	if err != nil {
		return model.Media{}, err
	}

	// 分享页偶发返回一版没有作品数据的页面(实测遇到过一次, 重发就正常),
	// 所以给它两次机会; 失败一次就报错的话, 用户看到的是"这条链接解析不了"。
	const attempts = 2
	var lastErr error
	for i := 0; i < attempts; i++ {
		// 移动端分享页是唯一稳定给出作品数据的入口, 桌面页要签名。
		resp, err := d.env.Get("https://www.iesdouyin.com/share/video/"+id+"/", d.headers())
		if err != nil {
			return model.Media{}, err
		}
		item, err := douyinItem(resp.Text())
		if err == nil {
			return douyinMedia(item, id), nil
		}
		lastErr = err
	}
	return model.Media{}, lastErr
}

func (d *douyin) identify(rawURL string) (string, error) {
	if id := douyinIDPattern.FindStringSubmatch(rawURL); id != nil {
		return id[1], nil
	}
	resp, err := d.env.Get(rawURL, d.headers())
	if err != nil {
		return "", fmt.Errorf("短链展开失败: %w", err)
	}
	if id := douyinIDPattern.FindStringSubmatch(resp.URL); id != nil {
		return id[1], nil
	}
	if id := douyinIDPattern.FindStringSubmatch(resp.Text()); id != nil {
		return id[1], nil
	}
	return "", errors.New("没从这个链接里认出抖音作品号")
}

func (d *douyin) headers() map[string]string {
	return map[string]string{
		"User-Agent": byteDanceUA,
		"Referer":    "https://www.douyin.com/",
	}
}

// douyinItem 从分享页里挖出作品对象: 页面把作品塞在层层 loaderData 里, 位置随版本变,
// 所以按"有 desc 又有 video"这个特征找, 不写死路径。
func douyinItem(html string) (map[string]any, error) {
	raw, ok := jsonAfter(html, "window._ROUTER_DATA")
	if !ok {
		raw, ok = jsonAfter(html, "__pace_f.push")
	}
	if !ok {
		return nil, errors.New("抖音页面里没找到作品数据")
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		// 有的页面把 JSON 塞在字符串里, 再试一层
		var nested string
		if json.Unmarshal(raw, &nested) != nil {
			return nil, errors.New("抖音作品数据解析失败")
		}
		if err := json.Unmarshal([]byte(nested), &root); err != nil {
			return nil, errors.New("抖音作品数据解析失败")
		}
	}
	item := findMap(root, []string{"desc"}, []string{"video", "images"})
	if item == nil {
		return nil, errors.New("抖音没给作品详情(可能已删除或仅自己可见)")
	}
	return item, nil
}

func douyinMedia(item map[string]any, id string) model.Media {
	video := dict(item["video"])
	media := model.Media{
		Platform: "抖音",
		Title:    firstLine(str(item["desc"])),
		Author:   firstString(dict(item["author"]), "nickname", "unique_id"),
		Cover:    firstHTTP(video["cover"], video["origin_cover"], video["dynamic_cover"]),
		Duration: number(video["duration"]) / 1000,
		PageURL:  "https://www.douyin.com/video/" + id,
	}
	if media.Title == "" {
		media.Title = "抖音视频"
	}

	if images := list(item["images"]); len(images) > 0 {
		media.Images = collectURLs(images)
		return media
	}
	if url := douyinPlayURL(video); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": byteDanceUA, "Referer": "https://www.douyin.com/"},
		}
	}
	return media
}

// douyinPlayURL 取播放地址并顺手去水印: 分享页给的是带 playwm 的地址, 换成 play 就是原片。
func douyinPlayURL(video map[string]any) string {
	raw := firstHTTP(video["play_addr"], video["play_addr_h264"], video["download_addr"])
	if raw == "" {
		return ""
	}
	return replacePlaywm(raw)
}

// collectURLs 从图集里按顺序取每张图的地址。
func collectURLs(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if url := firstHTTP(item); url != "" {
			out = append(out, url)
		}
	}
	return out
}

func dict(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// firstHTTP 递归找第一个 http(s) 地址: 封面与图集的字段名各版本不一, 按值找最稳。
func firstHTTP(nodes ...any) string {
	for _, node := range nodes {
		switch v := node.(type) {
		case string:
			if strings.HasPrefix(v, "http") {
				return v
			}
		case []any:
			for _, child := range v {
				if found := firstHTTP(child); found != "" {
					return found
				}
			}
		case map[string]any:
			if urls := list(v["url_list"]); urls != nil {
				if found := firstHTTP(urls); found != "" {
					return found
				}
			}
			for _, child := range v {
				if found := firstHTTP(child); found != "" {
					return found
				}
			}
		}
	}
	return ""
}
