package parse

import (
	"fmt"
	"net/url"
	"strings"
)

// hostOf 取域名, 解析失败返回空串(调用方按"没有域名"处理)。
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// normalizeURL 补全协议相对地址: 站点常给 //i0.hdslb.com/... 这种形状。
func normalizeURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

// firstLine 只留第一行并裁到合理长度 —— 简介动不动几千字, 塞进卡片会把整条消息撑爆。
func firstLine(text string) string {
	line := strings.TrimSpace(text)
	if at := strings.IndexAny(line, "\r\n"); at >= 0 {
		line = strings.TrimSpace(line[:at])
	}
	return truncate(line, 120)
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// count 把播放量这类数字缩成中文习惯的写法。
func count(n int64) string {
	switch {
	case n >= 100000000:
		return fmt.Sprintf("%.1f亿", float64(n)/100000000)
	case n >= 10000:
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// nonEmpty 返回第一个非空白的值; 全空返回空串。
func nonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// number 从 JSON 数字里取值; 平台一会儿给整数一会儿给浮点, 统一收。
func number(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
