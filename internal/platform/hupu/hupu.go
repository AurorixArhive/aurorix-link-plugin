package hupu

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Linux; Android 15; SM-G998B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Mobile Safari/537.36 Edg/132.0.0.0",
}

const salt = "HUPU_SALT_AKJfoiwer394Jeiow4u309"

var platform = model.Platform{Name: "hupu", DisplayName: "虎扑"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "m.hupu.com/bbs-share",
			Pattern: parse.Compile(`bbs-share/(?P<topic_id>\d+)(?:\.html)?`),
			Handle:  p.bbs,
		},
		{
			Keyword: "m.hupu.com/bbs",
			Pattern: parse.Compile(`bbs/(?P<topic_id>\d+)(?:\.html)?`),
			Handle:  p.bbs,
		},
		{
			Keyword: "bbs.hupu.com",
			Pattern: parse.Compile(`(?P<topic_id>\d+)(?:\.html)?`),
			Handle:  p.bbs,
		},
	}
}

type bbsData struct {
	OfflineData struct {
		Data struct {
			FID        string `json:"fid"`
			TID        string `json:"tid"`
			Title      string `json:"title"`
			Content    string `json:"content"`
			ShareNum   int64  `json:"share_num"`
			Replies    string `json:"replies"`
			CreateTime int64  `json:"create_time"`
		} `json:"data"`
	} `json:"offline_data"`
	Author struct {
		Name   string `json:"name"`
		Header string `json:"header"`
		View   int64  `json:"view"`
		PUID   int64  `json:"puid"`
	} `json:"author"`
	RecommendNum string `json:"recommend_num"`
	VideoInfo    *struct {
		Img      string `json:"img"`
		Src      string `json:"src"`
		Duration string `json:"duration"`
		Size     string `json:"size"`
		PlayNum  string `json:"play_num"`
	} `json:"video_info"`
	TID string `json:"tid"`
	FID string `json:"fid"`
}

func (p *Parser) fetch(endpoint string, params map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + salt))
	query := strings.Join(append(parts, "sign="+hex.EncodeToString(sum[:])), "&")
	return p.client.Get(endpoint+"?"+query, headers)
}

func (p *Parser) bbs(ctx context.Context, m *parse.Match) (*model.Result, error) {
	topicID := m.Get("topic_id")
	raw, err := p.fetch("https://bbs.mobileapi.hupu.com/1/7.5.51/threads/"+topicID, nil)
	if err != nil {
		return nil, err
	}
	var got bbsData
	if err := netx.Decode(raw, &got); err != nil {
		return nil, err
	}

	content := richContent(got.OfflineData.Data.Content)
	if got.VideoInfo != nil {
		content = append(content, model.Video{
			URL:      got.VideoInfo.Src,
			Cover:    got.VideoInfo.Img,
			Duration: parseFloat(got.VideoInfo.Duration),
			Headers:  headers,
		})
	}

	return &model.Result{
		Platform: platform,
		URL:      "https://m.hupu.com/bbs/" + got.TID + ".html",
		Title:    got.OfflineData.Data.Title,
		Author: model.Author{
			Name:   got.Author.Name,
			ID:     strconv.FormatInt(got.Author.PUID, 10),
			Avatar: got.Author.Header,
		},
		Stats: model.Stats{
			View:    htmlx.Num(got.Author.View),
			Like:    got.RecommendNum,
			Share:   htmlx.Num(got.OfflineData.Data.ShareNum),
			Comment: got.OfflineData.Data.Replies,
		},
		Timestamp: got.OfflineData.Data.CreateTime,
		Content:   content,
	}, nil
}

func parseFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return parsed
}

var newlineTags = map[string]bool{
	"p": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "li": true, "blockquote": true, "pre": true, "hr": true,
}

func richContent(raw string) []model.Content {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	out := make([]model.Content, 0, 8)
	buffer := make([]string, 0, 8)
	seen := map[*html.Node]bool{}

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		if text := strings.TrimSpace(strings.Join(buffer, "")); text != "" {
			out = append(out, model.Text{Value: text})
		}
		buffer = buffer[:0]
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.ElementNode:
			if newlineTags[n.Data] {
				buffer = append(buffer, "\n")
			} else if n.Data == "video" {
				flush()
				out = append(out, model.Video{URL: attr(n, "src"), Cover: attr(n, "poster"), Headers: headers})
				return
			} else if n.Data == "img" {
				if src := firstAttr(n, "data-gif", "data-src", "src"); src != "" {
					flush()
					out = append(out, model.Image{URL: src})
				}
				return
			}
		case html.TextNode:
			if anchor := parentTag(n, "a"); anchor != nil {
				if !seen[anchor] {
					seen[anchor] = true
					if text := anchorText(anchor, "https://bbs.hupu.com/"); text != "" {
						buffer = append(buffer, text)
					}
				}
				return
			}
			if text := htmlx.Compact(n.Data); text != "" {
				buffer = append(buffer, text)
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	flush()
	return out
}

func anchorText(n *html.Node, base string) string {
	if findElement(n, "img") != nil {
		return ""
	}
	label := nodeText(n)
	if label == "" {
		return ""
	}
	href := attr(n, "href")
	if href == "" || strings.HasPrefix(href, "#") {
		return label
	}
	return label + " (" + resolveURL(base, href) + ")"
}

func resolveURL(base, href string) string {
	parsedBase, err := url.Parse(base)
	if err != nil {
		return href
	}
	parsedHref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return parsedBase.ResolveReference(parsedHref).String()
}

func nodeText(n *html.Node) string {
	parts := make([]string, 0, 4)
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			if text := htmlx.Compact(cur.Data); text != "" {
				parts = append(parts, text)
			}
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(parts, " ")
}

func parentTag(n *html.Node, name string) *html.Node {
	for parent := n.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == name {
			return parent
		}
	}
	return nil
}

func findElement(n *html.Node, name string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if found != nil {
			return
		}
		if cur.Type == html.ElementNode && cur.Data == name {
			found = cur
			return
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
}

func attr(n *html.Node, key string) string {
	for _, item := range n.Attr {
		if strings.EqualFold(item.Key, key) {
			return strings.TrimSpace(item.Val)
		}
	}
	return ""
}

func firstAttr(n *html.Node, keys ...string) string {
	for _, key := range keys {
		if value := attr(n, key); value != "" {
			return value
		}
	}
	return ""
}

func init() { parse.Register(New) }
