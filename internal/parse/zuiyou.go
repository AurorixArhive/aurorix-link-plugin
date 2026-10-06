package parse

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"video-plugin/internal/model"
)

var (
	zuiyouVideoRE  = regexp.MustCompile(`<video[^>]+src="([^"]+)"`)
	zuiyouPosterRE = regexp.MustCompile(`<video[^>]+poster="([^"]+)"`)
	zuiyouTitleRE  = regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`)
	zuiyouAuthorRE = regexp.MustCompile(`SharePostCard__name[^>]*>(.*?)<`)
	zuiyouImgRE    = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
)

type zuiyou struct {
	env *env
}

func newZuiyou(env *env) *zuiyou { return &zuiyou{env: env} }

func (z *zuiyou) Parse(rawURL string) (model.Media, error) {
	// 分享页是服务端渲染的, 作品信息直接在 HTML 里
	resp, err := z.env.Get(rawURL, z.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("最右页面打不开: %w", err)
	}
	html := resp.Text()
	if !strings.Contains(html, "SharePostCard") && !strings.Contains(html, "<video") {
		return model.Media{}, errors.New("最右页面里没找到作品数据(可能已删除或需要重新复制链接)")
	}
	return zuiyouMedia(html, rawURL), nil
}

func (z *zuiyou) headers() map[string]string {
	return map[string]string{
		"User-Agent": MobileUA,
		"Referer":    "https://share.xiaochuankeji.cn/",
	}
}

func zuiyouMedia(html, pageURL string) model.Media {
	media := model.Media{
		Platform: "最右",
		Title:    firstLine(zuiyouText(html, zuiyouTitleRE)),
		Author:   firstLine(zuiyouText(html, zuiyouAuthorRE)),
		Desc:     firstLine(zuiyouText(html, zuiyouTitleRE)),
		Cover:    firstHTTP(match(zuiyouPosterRE, html)),
		PageURL:  pageURL,
	}
	if media.Title == "" {
		media.Title = "最右内容"
	}

	if url := match(zuiyouVideoRE, html); url != "" {
		media.Video = &model.Video{
			URL:     normalizeURL(url),
			Headers: map[string]string{"User-Agent": MobileUA, "Referer": "https://share.xiaochuankeji.cn/"},
		}
		return media
	}
	media.Images = zuiyouImages(html)
	if media.Cover == "" && len(media.Images) > 0 {
		media.Cover = media.Images[0]
	}
	return media
}

// zuiyouImages 只认站内图片, 页面上还混着头像与图标。
func zuiyouImages(html string) []string {
	out := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	for _, m := range zuiyouImgRE.FindAllStringSubmatch(html, -1) {
		url := normalizeURL(m[1])
		if !strings.Contains(url, "/img/view/id") || seen[url] {
			continue
		}
		seen[url] = true
		out = append(out, url)
	}
	return out
}

func match(re *regexp.Regexp, text string) string {
	if m := re.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func zuiyouText(html string, re *regexp.Regexp) string {
	return plainText(match(re, html))
}
