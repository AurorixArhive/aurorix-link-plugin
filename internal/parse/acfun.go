package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"video-plugin/internal/model"
)

var acfunIDPattern = regexp.MustCompile(`/v/ac(\d+)`)

type acfun struct {
	env *env
}

func newAcfun(env *env) *acfun { return &acfun{env: env} }

func (a *acfun) Parse(rawURL string) (model.Media, error) {
	id := acfunID(rawURL)
	if id == "" {
		return model.Media{}, errors.New("没从这个链接里认出 AcFun 作品号")
	}
	// ajaxpipe 是播放页自己取数据用的入口, 返回的作品信息比服务端渲染的页面全
	target := "https://www.acfun.cn/v/ac" + id + "?quickViewId=videoInfo_new&ajaxpipe=1"
	resp, err := a.env.Get(target, a.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("AcFun 页面打不开: %w", err)
	}
	info := acfunInfo(resp.Text())
	if info == nil {
		return model.Media{}, errors.New("AcFun 页面里没找到作品数据")
	}
	return acfunMedia(info, id), nil
}

func (a *acfun) headers() map[string]string {
	return map[string]string{
		"User-Agent": BrowserUA,
		"Referer":    "https://www.acfun.cn/",
	}
}

func acfunID(rawURL string) string {
	if m := acfunIDPattern.FindStringSubmatch(rawURL); m != nil {
		return m[1]
	}
	return ""
}

// acfunInfo 从 ajaxpipe 的返回里取作品对象。外面裹着 CDATA 与赋值语句, 中间才是 JSON。
func acfunInfo(body string) map[string]any {
	raw, ok := jsonAfter(body, "videoInfo")
	if !ok {
		// 没有赋值前缀时, 整段本身就是 JSON
		if at := strings.IndexByte(body, '{'); at >= 0 {
			raw, ok = jsonAfter(body[:at+1], "{")
		}
	}
	if !ok {
		return nil
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	return findMap(root, []string{"title"}, []string{"videoId", "dougaId", "description", "user"})
}

func acfunMedia(info map[string]any, id string) model.Media {
	media := model.Media{
		Platform: "AcFun",
		Title:    firstLine(str(info["title"])),
		Author:   firstString(dict(info["user"]), "name", "username"),
		Desc:     firstLine(str(info["description"])),
		PageURL:  "https://www.acfun.cn/v/ac" + id,
	}
	if media.Title == "" {
		media.Title = "AcFun 视频"
	}

	duration := number(info["duration"])
	if duration > 10000 {
		duration /= 1000
	}
	media.Duration = duration
	media.Cover = firstHTTP(info["coverUrl"], info["image"], info["cover"])

	if url := acfunPlayURL(info); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": BrowserUA, "Referer": "https://www.acfun.cn/"},
		}
	}
	return media
}

// acfunPlayURL 从 ksPlayJson 里挑一路可播地址。
// 这个字段本身是被转义过的 JSON 字符串, 得再解一层。
func acfunPlayURL(info map[string]any) string {
	raw := firstString(info, "ksPlayJson", "playJson")
	if raw == "" {
		return firstHTTP(info["videoUrl"], info["src"])
	}
	var body struct {
		AdaptationSet []struct {
			Representation []struct {
				URL      string `json:"url"`
				Backup   string `json:"backupUrl"`
				Bitrate  int    `json:"bitrate"`
				Quality  int    `json:"qualityType"`
				M3U8     string `json:"m3u8Slice"`
				PlayAddr string `json:"playUrl"`
			} `json:"representation"`
		} `json:"adaptationSet"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil {
		return ""
	}
	best := ""
	bestQuality := -1
	for _, set := range body.AdaptationSet {
		for _, rep := range set.Representation {
			url := nonEmpty(rep.URL, rep.Backup, rep.PlayAddr, rep.M3U8)
			if url == "" || strings.Contains(url, ".m3u8") {
				// m3u8 是分片清单, 交给 QQ 会当普通文件, 这里只认单文件 MP4
				continue
			}
			if rep.Quality > bestQuality {
				bestQuality = rep.Quality
				best = url
			}
		}
	}
	return best
}
