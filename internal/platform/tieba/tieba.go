package tieba

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
	"golang.org/x/net/html"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Referer":    "https://tieba.baidu.com/",
}

const (
	pageAPI = "https://tieba.baidu.com/mo/q/m?kz="
	homeURL = "https://www.baidu.com/"
)

var platform = model.Platform{Name: "tieba", DisplayName: "百度贴吧"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{{
		Keyword: "tieba.baidu.com",
		Pattern: parse.Compile(`tieba\.baidu\.com/p/(?P<post_id>\d+)`),
		Handle:  p.post,
	}}
}

type floorInfo struct {
	User     string `json:"un"`
	NameShow string `json:"name_show"`
	Portrait string `json:"portrait"`
}

func (p *Parser) post(ctx context.Context, m *parse.Match) (*model.Result, error) {
	postID := m.Get("post_id")
	_, _ = p.client.Get(homeURL, nil)

	raw, err := p.client.Get(pageAPI+postID, nil)
	if err != nil {
		return nil, err
	}
	if bytes.Contains(raw, []byte("百度安全验证")) {
		return nil, errBlocked
	}

	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	first := find(doc, func(n *html.Node) bool { return attr(n, "data-info") != "" })
	if first == nil {
		return nil, errBlocked
	}

	var info floorInfo
	_ = json.Unmarshal([]byte(attr(first, "data-info")), &info)

	content := contentOf(find(first, func(n *html.Node) bool {
		return n.Data == "div" && hasClass(n, "content")
	}))
	if len(content) == 0 {
		content = contentOf(first)
	}

	portrait := info.Portrait
	if i := strings.IndexByte(portrait, '?'); i >= 0 {
		portrait = portrait[:i]
	}
	name := info.NameShow
	if name == "" {
		name = info.User
	}
	avatar := ""
	if portrait != "" {
		avatar = "http://tb.himg.baidu.com/sys/portraith/item/" + portrait
	}

	stats := model.Stats{}
	if reply := find(first, func(n *html.Node) bool { return hasClass(n, "btn_reply") }); reply != nil {
		stats.Comment = htmlx.Num(count(textOf(reply)))
	}

	return &model.Result{
		Platform: platform,
		URL:      "https://tieba.baidu.com/p/" + postID,
		Title:    titleOf(doc),
		Author:   model.Author{Name: name, Avatar: avatar, ID: portrait},
		Stats:    stats,
		Content:  content,
	}, nil
}

func titleOf(doc *html.Node) string {
	title := strings.TrimSpace(textOf(find(doc, func(n *html.Node) bool { return n.Data == "title" })))
	return strings.TrimSuffix(title, "_百度贴吧")
}

func contentOf(root *html.Node) []model.Content {
	if root == nil {
		return nil
	}
	out := make([]model.Content, 0, 8)
	var buf strings.Builder
	flush := func() {
		if text := htmlx.Compact(buf.String()); text != "" {
			out = append(out, model.Text{Value: text})
		}
		buf.Reset()
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "div" && hasClass(n, "pb_img_item") {
				flush()
				if src := imageURL(attr(n, "data-url")); src != "" {
					out = append(out, model.Image{URL: src})
				}
				return
			}
			if n.Data == "br" || n.Data == "p" {
				buf.WriteString("\n")
			}
			if n.Data == "script" || n.Data == "style" || attr(n, "id") == "diversBanner" {
				return
			}
		}
		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	flush()
	return out
}

func imageURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil {
		if src := parsed.Query().Get("src"); src != "" {
			return src
		}
	}
	return raw
}

func find(root *html.Node, match func(*html.Node) bool) *html.Node {
	var found *html.Node
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if match(n) {
			found = n
			return true
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if walk(child) {
				return true
			}
		}
		return false
	}
	walk(root)
	return found
}

func textOf(n *html.Node) string {
	if n == nil {
		return ""
	}
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			buf.WriteString(cur.Data)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return buf.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return strings.Contains(attr(n, "class"), class)
}

func count(text string) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	return value
}

type errorString string

func (e errorString) Error() string { return string(e) }

var errBlocked = errorString("贴吧网页需要百度安全验证, 拿不到帖子内容")
