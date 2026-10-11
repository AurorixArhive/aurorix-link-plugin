// Package rednote 解析小红书: 短链用 GET 跟到底, 再从页面的 __INITIAL_STATE__ 里按特征找笔记对象。
package rednote

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

// mobileUA 笔记页只在移动端身份下给完整数据; xhslink 短链用桌面 UA 会被踢到 404。
const mobileUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1"

var headers = map[string]string{
	"User-Agent": mobileUA,
	"Referer":    "https://www.xiaohongshu.com/",
}

// noteBase 笔记页的域名; 测试里会指到假服务器上, 所以留成变量。
var noteBase = "https://www.xiaohongshu.com"

var platform = model.Platform{Name: "rednote", DisplayName: "小红书"}

type Parser struct {
	client *netx.Client
	cookie string
}

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) SetCookie(value string) { p.cookie = strings.TrimSpace(value) }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "xhslink.cn",
			Pattern: parse.Compile(`xhslink\.cn/[A-Za-z0-9._?%&+=/#@-]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "xhslink.com",
			Pattern: parse.Compile(`xhslink\.com/[A-Za-z0-9._?%&+=/#@-]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "xiaohongshu.com",
			Pattern: parse.Compile(`(?P<type>explore|search_result|discovery/item)/(?P<note_id>[0-9a-zA-Z]+)[^\s]*`),
			Params:  map[string]parse.ParamRule{"xsec_token": {}},
			Handle:  p.note,
		},
	}
}

func (p *Parser) requestHeaders() map[string]string {
	if p.cookie == "" {
		return headers
	}
	merged := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		merged[k] = v
	}
	merged["Cookie"] = p.cookie
	return merged
}

// shortLink 短链必须用 GET 跟: 跳过去那一页就是笔记页, 顺手把它当正文用掉。
func (p *Parser) shortLink(ctx context.Context, m *parse.Match) (*model.Result, error) {
	target := m.URL
	if !strings.HasPrefix(target, "http") {
		target = "https://" + target
	}
	return p.page(ctx, target)
}

// note 不另编地址, 就用分享里那条链接的规范形式去请求 —— 改名前的做法。
func (p *Parser) note(ctx context.Context, m *parse.Match) (*model.Result, error) {
	noteID := m.Get("note_id")
	if noteID == "" {
		return nil, errNoData
	}
	target := noteBase + "/explore/" + noteID
	if token := m.Get("xsec_token"); token != "" {
		target += "?xsec_token=" + url.QueryEscape(token) + "&xsec_source=pc_share"
	}
	return p.page(ctx, target)
}

func (p *Parser) page(ctx context.Context, target string) (*model.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body, final, err := p.client.GetFinal(target, p.requestHeaders())
	if err != nil {
		return nil, err
	}
	note, err := noteOf(string(body))
	if err != nil {
		return nil, err
	}
	return media(note, final), nil
}

// jsonAfter 从页面里抓 "marker = {...}" 那段 JSON: 括号配对才截得全, 正则给的多半是残段。
func jsonAfter(page, marker string) ([]byte, bool) {
	at := strings.Index(page, marker)
	if at < 0 {
		return nil, false
	}
	start := strings.IndexByte(page[at+len(marker):], '{')
	if start < 0 {
		return nil, false
	}
	start += at + len(marker)

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(page); i++ {
		c := page[i]
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
				return []byte(page[start : i+1]), true
			}
		}
	}
	return nil, false
}

// findMap 递归找第一个含 required 全部键、且含 anyOf 至少一个键的对象: 站点每版埋的深度都不同。
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

func noteOf(page string) (map[string]any, error) {
	raw, ok := jsonAfter(page, "window.__INITIAL_STATE__")
	if !ok {
		return nil, errNoState
	}
	// 页面里写的是 JS 的 undefined, 不是合法 JSON
	raw = bytes.ReplaceAll(raw, []byte("undefined"), []byte("null"))
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, errNoState
	}
	note := findMap(root, []string{"noteId"}, []string{"imageList", "video", "desc"})
	if note == nil {
		return nil, errNoData
	}
	return note, nil
}

func media(note map[string]any, pageURL string) *model.Result {
	noteID := str(note["noteId"])
	title := firstLine(nonEmpty(str(note["title"]), str(note["desc"])))
	if title == "" {
		title = "小红书笔记"
	}
	user := dict(note["user"])
	stats := dict(note["interactInfo"])

	content := make([]model.Content, 0, 4)
	if desc := htmlx.Compact(str(note["desc"])); desc != "" {
		content = append(content, model.Text{Value: desc})
	}
	if link := videoURL(note); link != "" {
		content = append(content, model.Video{URL: link, Cover: firstImage(note), Headers: headers})
	} else {
		for _, link := range imageURLs(note) {
			content = append(content, model.Image{URL: link})
		}
	}

	return &model.Result{
		Platform: platform,
		URL:      nonEmpty(noteBase+"/explore/"+noteID, pageURL),
		Title:    title,
		Author: model.Author{
			Name:   firstString(user, "nickname", "nickName"),
			Avatar: firstHTTP(user["avatar"]),
		},
		Stats: model.Stats{
			Like:    str(stats["likedCount"]),
			Collect: str(stats["collectedCount"]),
			Comment: str(stats["commentCount"]),
			Share:   str(stats["shareCount"]),
		},
		Timestamp: numberOf(note["lastUpdateTime"]) / 1000,
		Content:   content,
	}
}

// videoURL 按 h264 → h265 → av1 取: h264 兼容性最好, 别的有些客户端放不了。
func videoURL(note map[string]any) string {
	stream := dict(dict(dict(note["video"])["media"])["stream"])
	if stream == nil {
		return ""
	}
	for _, codec := range []string{"h264", "h265", "av1"} {
		if link := firstHTTP(stream[codec]); link != "" {
			return link
		}
	}
	return ""
}

// imageURLs 图集的图: 字段名各版本不一, 按值找最稳。
func imageURLs(note map[string]any) []string {
	items := list(note["imageList"])
	out := make([]string, 0, len(items))
	for _, item := range items {
		entry := dict(item)
		if link := firstHTTP(entry["urlDefault"], entry["urlPre"], entry["url"]); link != "" {
			out = append(out, link)
		}
	}
	return out
}

func firstImage(note map[string]any) string {
	if links := imageURLs(note); len(links) > 0 {
		return links[0]
	}
	return ""
}

// firstHTTP 递归找第一个 http(s) 地址: 各版本字段名不一, 按值找最稳。
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

// firstString 在嵌套结构里深度优先找第一个命中的字符串。
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

func numberOf(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

func nonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// firstLine 简介动不动上千字, 塞进标题会把卡片撑爆。
func firstLine(text string) string {
	line := strings.TrimSpace(text)
	if at := strings.IndexAny(line, "\r\n"); at >= 0 {
		line = strings.TrimSpace(line[:at])
	}
	runes := []rune(line)
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return line
}

var (
	errNoState = errorString("小红书页面里没有 __INITIAL_STATE__")
	errNoData  = errorString("小红书笔记数据为空, 可能链接失效或需要 Cookie")
)

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
