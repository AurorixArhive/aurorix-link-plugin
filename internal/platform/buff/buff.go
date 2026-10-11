package buff

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://buff.163.com/",
}

var platform = model.Platform{Name: "buff", DisplayName: "BUFF"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "buff.163.com/s/news-detail_share.html",
			Pattern: parse.Domain("buff.163.com/s/news-detail_share.html"),
			Params: map[string]parse.ParamRule{
				"article_id":   {},
				"comment_type": {Equals: "228"},
			},
			Handle: p.video,
		},
		{
			Keyword: "buff.163.com/s/preview_share.html",
			Pattern: parse.Domain("buff.163.com/s/preview_share.html"),
			Params: map[string]parse.ParamRule{
				"game":         {},
				"preview_id":   {},
				"comment_type": {Equals: "216"},
			},
			Handle: p.gallery,
		},
		{
			Keyword: "buff.163.com/s/news-detail_share.html",
			Pattern: parse.Domain("buff.163.com/s/news-detail_share.html"),
			Params: map[string]parse.ParamRule{
				"article_id":   {},
				"comment_type": {Equals: "211"},
			},
			Handle: p.news,
		},
		{
			Keyword: "buff.163.com/s/topic-detail_share.html",
			Pattern: parse.Domain("buff.163.com/s/topic-detail_share.html"),
			Params: map[string]parse.ParamRule{
				"social_topic_post_id": {},
				"comment_type":         {Equals: "239"},
			},
			Handle: p.topic,
		},
	}
}

func (p *Parser) fetch(rawURL string, params map[string]string, out any) error {
	query := url.Values{}
	for key, value := range params {
		query.Set(key, value)
	}
	var got struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := p.client.GetJSON(rawURL+"?"+query.Encode(), &got, nil); err != nil {
		return err
	}
	if got.Code != "OK" {
		return errorString("BUFF 接口返回 " + got.Code)
	}
	return netx.Decode(got.Data, out)
}

type shareData struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type userInfo struct {
	Avatar     string `json:"avatar"`
	IPLocation string `json:"ip_location"`
	Nickname   string `json:"nickname"`
	UserID     string `json:"user_id"`
}

type videoItem struct {
	Duration string `json:"duration"`
	IconURL  string `json:"icon_url"`
	VideoURL string `json:"video_url"`
}

type videoData struct {
	Author      string      `json:"author"`
	UserID      string      `json:"user_id"`
	Avatar      string      `json:"avatar"`
	Body        string      `json:"body"`
	IPLocation  string      `json:"ip_location"`
	PublishTime int64       `json:"publish_time"`
	Replies     int64       `json:"replies"`
	UpsNum      int64       `json:"ups_num"`
	Views       int64       `json:"views"`
	Video       []videoItem `json:"video"`
	ShareData   shareData   `json:"share_data"`
}

type galleryData struct {
	Preview struct {
		Description string    `json:"description"`
		IconURL     string    `json:"icon_url"`
		UserID      string    `json:"user_id"`
		PublishTime int64     `json:"publish_time"`
		UpsNum      int64     `json:"ups_num"`
		ShareData   shareData `json:"share_data"`
	} `json:"preview"`
	UserInfos map[string]userInfo `json:"user_infos"`
}

type newsData struct {
	Author      string    `json:"author"`
	UserID      string    `json:"user_id"`
	Avatar      string    `json:"avatar"`
	Body        string    `json:"body"`
	IPLocation  string    `json:"ip_location"`
	PublishTime int64     `json:"publish_time"`
	Replies     int64     `json:"replies"`
	Title       string    `json:"title"`
	UpsNum      int64     `json:"ups_num"`
	Views       int64     `json:"views"`
	ShareData   shareData `json:"share_data"`
}

type topicData struct {
	UserInfos map[string]userInfo `json:"user_infos"`
	Items     []struct {
		AuthorID string `json:"author_id"`
		Content  string `json:"content"`
		Pictures []struct {
			ImageURL string `json:"image_url"`
			IsEmoji  bool   `json:"is_emoji"`
		} `json:"pictures"`
		PublishTime int64     `json:"publish_time"`
		Replies     int64     `json:"replies"`
		UpsNum      int64     `json:"ups_num"`
		ShareData   shareData `json:"share_data"`
	} `json:"items"`
}

func (p *Parser) video(ctx context.Context, m *parse.Match) (*model.Result, error) {
	var got videoData
	err := p.fetch("https://buff.163.com/api/news/share/detail", map[string]string{"article_id": m.Get("article_id")}, &got)
	if err != nil {
		return nil, err
	}
	content := make([]model.Content, 0, len(got.Video)+1)
	if text := htmlx.Text(got.Body); text != "" {
		content = append(content, model.Text{Value: text})
	}
	for _, item := range got.Video {
		duration, _ := strconv.ParseFloat(item.Duration, 64)
		content = append(content, model.Video{URL: item.VideoURL, Cover: item.IconURL, Duration: duration, Headers: headers})
	}
	return &model.Result{
		Platform:  platform,
		URL:       got.ShareData.URL,
		Title:     got.ShareData.Title,
		Timestamp: got.PublishTime,
		Author:    model.Author{Name: got.Author, ID: got.UserID, Avatar: got.Avatar},
		Stats: model.Stats{
			View:    htmlx.Num(got.Views),
			Like:    htmlx.Num(got.UpsNum),
			Comment: htmlx.Num(got.Replies),
		},
		Content: content,
	}, nil
}

func (p *Parser) gallery(ctx context.Context, m *parse.Match) (*model.Result, error) {
	var got galleryData
	err := p.fetch("https://buff.163.com/api/market/preview/share_detail", map[string]string{
		"preview_id": m.Get("preview_id"),
		"game":       m.Get("game"),
	}, &got)
	if err != nil {
		return nil, err
	}
	content := make([]model.Content, 0, 2)
	if text := htmlx.Compact(got.Preview.Description); text != "" {
		content = append(content, model.Text{Value: text})
	}
	if got.Preview.IconURL != "" {
		content = append(content, model.Image{URL: got.Preview.IconURL})
	}
	author := got.UserInfos[got.Preview.UserID]
	return &model.Result{
		Platform:  platform,
		URL:       got.Preview.ShareData.URL,
		Title:     got.Preview.ShareData.Title,
		Timestamp: got.Preview.PublishTime,
		Author:    model.Author{Name: author.Nickname, ID: author.UserID, Avatar: author.Avatar},
		Stats: model.Stats{
			Like: htmlx.Num(got.Preview.UpsNum),
		},
		Content: content,
	}, nil
}

func (p *Parser) news(ctx context.Context, m *parse.Match) (*model.Result, error) {
	var got newsData
	err := p.fetch("https://buff.163.com/api/news/share/detail", map[string]string{"article_id": m.Get("article_id")}, &got)
	if err != nil {
		return nil, err
	}
	return &model.Result{
		Platform:  platform,
		URL:       got.ShareData.URL,
		Title:     got.ShareData.Title,
		Timestamp: got.PublishTime,
		Author:    model.Author{Name: got.Author, ID: got.UserID, Avatar: got.Avatar},
		Stats: model.Stats{
			View:    htmlx.Num(got.Views),
			Like:    htmlx.Num(got.UpsNum),
			Comment: htmlx.Num(got.Replies),
		},
		Content: newsContent(got.Body),
	}, nil
}

func (p *Parser) topic(ctx context.Context, m *parse.Match) (*model.Result, error) {
	var got topicData
	err := p.fetch("https://buff.163.com/api/topic/posts/detail", map[string]string{"social_topic_post_id": m.Get("social_topic_post_id")}, &got)
	if err != nil {
		return nil, err
	}
	if len(got.Items) == 0 {
		return nil, errorString("BUFF 帖子没有内容")
	}
	item := got.Items[0]
	author := got.UserInfos[item.AuthorID]
	content := make([]model.Content, 0, len(item.Pictures)+1)
	if text := htmlx.Compact(item.Content); text != "" {
		content = append(content, model.Text{Value: text})
	}
	for _, picture := range item.Pictures {
		if picture.ImageURL != "" {
			content = append(content, model.Image{URL: picture.ImageURL})
		}
	}
	return &model.Result{
		Platform:  platform,
		URL:       item.ShareData.URL,
		Timestamp: item.PublishTime,
		Author:    model.Author{Name: author.Nickname, ID: author.UserID, Avatar: author.Avatar},
		Stats: model.Stats{
			Like:    htmlx.Num(item.UpsNum),
			Comment: htmlx.Num(item.Replies),
		},
		Content: content,
	}, nil
}

var newlineTags = map[string]bool{
	"p": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "li": true, "blockquote": true, "pre": true, "hr": true,
}

func newsContent(raw string) []model.Content {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		if text := htmlx.Text(raw); text != "" {
			return []model.Content{model.Text{Value: text}}
		}
		return nil
	}
	var (
		out []model.Content
		buf strings.Builder
	)
	walkNews(root, &buf, &out)
	flushNews(&buf, &out)
	return out
}

func walkNews(n *html.Node, buf *strings.Builder, out *[]model.Content) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			if text := htmlx.Compact(c.Data); text != "" {
				buf.WriteString(text)
			}
		case html.ElementNode:
			name := strings.ToLower(c.Data)
			if newlineTags[name] {
				buf.WriteString("\n")
				walkNews(c, buf, out)
				continue
			}
			if name == "div" && hasClass(c, "video-content") {
				src := attr(c, "data-src")
				cover := ""
				if img := firstTag(c, "img"); img != nil {
					cover = attr(img, "src")
				}
				if src == "" || cover == "" {
					continue
				}
				flushNews(buf, out)
				*out = append(*out, model.Video{URL: src, Cover: cover, Headers: headers})
				continue
			}
			if name == "img" {
				if src := attr(c, "data-original"); src != "" {
					flushNews(buf, out)
					*out = append(*out, model.Image{URL: src})
				}
				continue
			}
			if name == "a" {
				if text := anchorText(c, "https://buff.163.com/"); text != "" {
					buf.WriteString(text)
					continue
				}
			}
			walkNews(c, buf, out)
		}
	}
}

func flushNews(buf *strings.Builder, out *[]model.Content) {
	if text := strings.TrimSpace(buf.String()); text != "" {
		*out = append(*out, model.Text{Value: text})
	}
	buf.Reset()
}

func anchorText(n *html.Node, base string) string {
	if firstTag(n, "img") != nil {
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
	baseURL, err := url.Parse(base)
	if err != nil {
		return label
	}
	ref, err := url.Parse(href)
	if err != nil {
		return label
	}
	return label + " (" + baseURL.ResolveReference(ref).String() + ")"
}

func nodeText(n *html.Node) string {
	parts := make([]string, 0, 4)
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				if text := htmlx.Compact(c.Data); text != "" {
					parts = append(parts, text)
				}
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return strings.Join(parts, " ")
}

func firstTag(n *html.Node, name string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, name) {
			return c
		}
		if found := firstTag(c, name); found != nil {
			return found
		}
	}
	return nil
}

func hasClass(n *html.Node, class string) bool {
	for _, part := range strings.Fields(attr(n, "class")) {
		if part == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
