package parse

import (
	"sort"
	"strings"
)

// BrowserUA 各站点对服务器默认 UA 的容忍度差别很大, 统一用桌面浏览器身份。
const BrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// MobileUA 分享页在移动端 UA 下才给完整数据, 小红书尤其明显。
const MobileUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1"

// byteDanceUA 字节系站点(抖音)对浏览器身份一律回 JS 挑战页, 这个老安卓 UA 才给 SSR 数据。
const byteDanceUA = "Mozilla/5.0 (Linux; Android 5.0; SM-G900P Build/LRX21T) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.25 Mobile Safari/537.36"

// jsonAfter 从页面里抓 "marker = {...}" 那段 JSON。
//
// 用括号配对而不是正则: 页面里的 JSON 自始至终都有同样的括号, 正则截出来的多半是残段,
// 报错时还会指向 JSON 语法, 与真实原因(截歪了)毫不相干。字符串里的括号与转义要跳过。
func jsonAfter(html, marker string) ([]byte, bool) {
	at := strings.Index(html, marker)
	if at < 0 {
		return nil, false
	}
	start := strings.IndexByte(html[at+len(marker):], '{')
	if start < 0 {
		return nil, false
	}
	start += at + len(marker)

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(html); i++ {
		c := html[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return []byte(html[start : i+1]), true
			}
		}
	}
	return nil, false
}

// firstString 在嵌套结构里按给定键名找第一个非空字符串, 深度优先。
// 各站点的作品对象位置常变, 写死完整路径的文字过几个月就失效。
func firstString(node any, keys ...string) string {
	switch v := node.(type) {
	case map[string]any:
		for _, key := range keys {
			if s, ok := v[key].(string); ok && s != "" {
				return s
			}
		}
		for _, key := range keys {
			if found := firstString(v[key], keys...); found != "" {
				return found
			}
		}
		for _, child := range v {
			if found := firstString(child, keys...); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range v {
			if found := firstString(child, keys...); found != "" {
				return found
			}
		}
	}
	return ""
}

// findMap 递归找第一个同时含有 required 全部键、且至少含 anyOf 一个键的对象。
//
// 站点把作品对象埋的层数各版本都不同, 按特征找比写死路径耐得住改版。
// map 的遍历顺序是随机的, 这里按 key 排序后再递归 —— 否则同一个页面可能这次解析出 A,
// 下次解析出 B, 问题只在"偶尔不对"里出现, 极难复现。
func findMap(node any, required, anyOf []string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if hasAll(v, required) && hasAny(v, anyOf) {
			return v
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if found := findMap(v[key], required, anyOf); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range v {
			if found := findMap(child, required, anyOf); found != nil {
				return found
			}
		}
	}
	return nil
}

func hasAll(node map[string]any, keys []string) bool {
	for _, key := range keys {
		if _, ok := node[key]; !ok {
			return false
		}
	}
	return true
}

func hasAny(node map[string]any, keys []string) bool {
	for _, key := range keys {
		if _, ok := node[key]; ok {
			return true
		}
	}
	return false
}
