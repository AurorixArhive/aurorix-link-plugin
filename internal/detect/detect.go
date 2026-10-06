// Package detect 负责一件事: 从一段文字或一组卡片链接里, 认出"这是哪个平台的作品链接"。
package detect

import (
	"net/url"
	"regexp"
	"strings"
)

const (
	Bilibili = "bilibili"
	Douyin   = "douyin"
	Kuaishou = "kuaishou"
	Xhs      = "xhs"
	Heybox   = "heybox"
	Acfun    = "acfun"
	Weibo    = "weibo"
	Miyoushe = "miyoushe"
	Weishi   = "weishi"
	Zuiyou   = "zuiyou"
	Ixigua   = "ixigua"
	Pipixia  = "pipixia"
	Xsj      = "xsj"
)

var names = map[string]string{
	Bilibili: "B站",
	Douyin:   "抖音",
	Kuaishou: "快手",
	Xhs:      "小红书",
	Heybox:   "小黑盒",
	Acfun:    "AcFun",
	Weibo:    "微博",
	Miyoushe: "米游社",
	Weishi:   "微视",
	Zuiyou:   "最右",
	Ixigua:   "西瓜视频",
	Pipixia:  "皮皮虾",
	Xsj:      "QQ小世界",
}

// Name 平台的中文名, 用于文案。
func Name(platform string) string {
	if n, ok := names[platform]; ok {
		return n
	}
	return "视频"
}

// Platforms 全部支持的平台, 供帮助文案与配置项遍历。
func Platforms() []string {
	return []string{
		Bilibili, Douyin, Kuaishou, Xhs, Heybox,
		Acfun, Weibo, Miyoushe, Weishi, Zuiyou,
		Ixigua, Pipixia, Xsj,
	}
}

// Hit 一个命中的作品链接。
type Hit struct {
	Platform string
	URL      string
}

var (
	urlPattern = regexp.MustCompile(`https?://[^\s'"<>()（）【】]+`)
	bvPattern  = regexp.MustCompile(`\bBV[0-9A-Za-z]{10}\b`)
	// 中文标点常被一起复制进来, 粘在链接尾巴上会让域名之后的部分全错
	trailing = "。，、；：？！）】》」』”’,.;:!?"
)

// Find 从消息正文里找第一个能解析的链接; 找不到返回 false(调用方什么都不做)。
func Find(text string) (Hit, bool) {
	for _, raw := range urlPattern.FindAllString(text, -1) {
		if hit, ok := Classify(raw); ok {
			return hit, true
		}
	}
	// 光秃秃的 BV 号: 很多人直接复制这个当链接发
	if bv := bvPattern.FindString(text); bv != "" {
		return Hit{Platform: Bilibili, URL: "https://www.bilibili.com/video/" + bv}, true
	}
	return Hit{}, false
}

// FindAll 找出正文里全部命中的链接, 按出现顺序去重。
func FindAll(text string) []Hit {
	hits := make([]Hit, 0, 2)
	seen := make(map[string]bool, 2)
	for _, raw := range urlPattern.FindAllString(text, -1) {
		hit, ok := Classify(raw)
		if !ok || seen[hit.URL] {
			continue
		}
		seen[hit.URL] = true
		hits = append(hits, hit)
	}
	return hits
}

// FromArk 从卡片里挑第一个能解析的链接。
// 卡片字段名随卡片类型而变, 所以逐个试而不是认键名。
func FromArk(urls []string) (Hit, bool) {
	for _, raw := range urls {
		if hit, ok := Classify(raw); ok {
			return hit, true
		}
	}
	return Hit{}, false
}

// Classify 判断单个链接属于哪个平台; 不认识的链接返回 false。
func Classify(raw string) (Hit, bool) {
	cleaned := strings.TrimRight(strings.TrimSpace(raw), trailing)
	u, err := url.Parse(cleaned)
	if err != nil || u.Host == "" {
		return Hit{}, false
	}
	host := strings.ToLower(u.Hostname())
	platform := platformOf(host)
	if platform == "" {
		return Hit{}, false
	}
	return Hit{Platform: platform, URL: cleaned}, true
}

func platformOf(host string) string {
	switch {
	case hostIs(host, "bilibili.com", "b23.tv", "bili2233.cn"):
		// 直播间是另一套接口, 不在这个插件的范围里
		if strings.HasPrefix(host, "live.") {
			return ""
		}
		return Bilibili
	case hostIs(host, "douyin.com", "iesdouyin.com"):
		if strings.HasPrefix(host, "live.") {
			return ""
		}
		return Douyin
	case hostIs(host, "kuaishou.com", "chenzhongtech.com", "gifshow.com"):
		return Kuaishou
	case hostIs(host, "xiaohongshu.com", "xhslink.com", "xhslink.cn"):
		return Xhs
	case hostIs(host, "xiaoheihe.cn", "heybox.cn"):
		return Heybox
	case hostIs(host, "acfun.cn", "acfun.com"):
		return Acfun
	case hostIs(host, "weibo.com", "weibo.cn"):
		return Weibo
	case hostIs(host, "miyoushe.com", "mihoyo.com"):
		return Miyoushe
	case hostIs(host, "weishi.qq.com"):
		return Weishi
	case hostIs(host, "xiaochuankeji.cn", "zuiyou.com"):
		return Zuiyou
	case hostIs(host, "ixigua.com"):
		return Ixigua
	case hostIs(host, "pipix.com", "pipigx.com"):
		return Pipixia
	case hostIs(host, "xsj.qq.com"):
		return Xsj
	}
	return ""
}

func hostIs(host string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
