package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"video-plugin/internal/model"
)

var pipixiaItemRE = regexp.MustCompile(`/item/(\d+)`)

type pipixia struct {
	env *env
}

func newPipixia(env *env) *pipixia { return &pipixia{env: env} }

func (p *pipixia) Parse(rawURL string) (model.Media, error) {
	// 短链先跟随跳转拿到带 item id 的落地页
	resp, err := p.env.Get(rawURL, p.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("皮皮虾页面打不开: %w", err)
	}
	itemID := pipixiaItemID(resp.URL)
	if itemID == "" {
		itemID = pipixiaItemID(resp.Text())
	}
	if itemID == "" {
		return model.Media{}, errors.New("没从这个链接里认出皮皮虾作品号")
	}
	if itemID != pipixiaItemID(resp.URL) {
		// 落地页不对时再取一次作品页, RENDER_DATA 只在作品页里有
		resp, err = p.env.Get("https://h5.pipix.com/item/"+itemID, p.headers())
		if err != nil {
			return model.Media{}, fmt.Errorf("皮皮虾作品页打不开: %w", err)
		}
	}

	detail, err := pipixiaDetail(resp.Text())
	if err != nil {
		return model.Media{}, err
	}
	return pipixiaMedia(detail, itemID), nil
}

func (p *pipixia) headers() map[string]string {
	return map[string]string{
		"User-Agent": MobileUA,
		"Referer":    "https://h5.pipix.com/",
	}
}

func pipixiaItemID(raw string) string {
	if m := pipixiaItemRE.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return ""
}

// pipixiaDetail 页面把整站数据塞在 RENDER_DATA 里, 值是 URL 编码过的 JSON。
func pipixiaDetail(html string) (map[string]any, error) {
	raw, ok := jsonAfter(html, `id="RENDER_DATA"`)
	if !ok {
		return nil, errors.New("皮皮虾页面里没找到作品数据(可能已删除)")
	}
	decoded, err := url.QueryUnescape(string(raw))
	if err != nil {
		decoded = string(raw)
	}
	var root any
	if json.Unmarshal([]byte(decoded), &root) != nil {
		return nil, errors.New("皮皮虾作品数据解析失败")
	}
	detail := findMap(root, []string{"item"}, []string{"video", "images", "content"})
	if detail == nil {
		return nil, errors.New("皮皮虾没给作品详情")
	}
	return detail, nil
}

func pipixiaMedia(detail map[string]any, itemID string) model.Media {
	item := dict(detail["item"])
	if item == nil {
		item = detail
	}
	media := model.Media{
		Platform: "皮皮虾",
		Title:    firstLine(nonEmpty(str(item["content"]), str(item["title"]))),
		Author:   firstString(dict(item["author"]), "name", "nickname"),
		Desc:     firstLine(str(item["content"])),
		PageURL:  "https://h5.pipix.com/item/" + itemID,
	}
	if media.Title == "" {
		media.Title = "皮皮虾内容"
	}

	if url := pipixiaVideoURL(detail, item); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": MobileUA, "Referer": "https://h5.pipix.com/"},
		}
		media.Cover = firstHTTP(dict(item["video"])["cover"], item["cover"])
		return media
	}
	for _, image := range list(item["images"]) {
		if url := firstHTTP(image); url != "" {
			media.Images = append(media.Images, url)
		}
	}
	return media
}

// pipixiaVideoURL 站内会在评论对象里再挂一份原片, 那份不带水印, 优先用它。
func pipixiaVideoURL(detail, item map[string]any) string {
	for _, cell := range list(detail["cell_comments"]) {
		comment := dict(dict(cell)["comment_info"])
		if url := firstHTTP(dict(comment["item"])["video"]); url != "" {
			return url
		}
	}
	return firstHTTP(dict(item["video"])["video"], dict(item["video"])["url"], item["video"])
}
