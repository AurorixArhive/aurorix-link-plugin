package illu

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
}

const (
	baseURL       = "https://api.illund.com"
	articleRouter = "/1/functions/getArticleByIdV2"
	drawingRouter = "/1/functions/getDrawingDetail"
	nonceChars    = "ABCDEFGHJKMNPQRSTWXYZabcdefhijkmnprstwxyz2345678"
	secretKey     = "d2b11e2f977ed97c"
)

var platform = model.Platform{Name: "illu", DisplayName: "ILLU"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "illund.com/share.html",
			Pattern: parse.Compile(`articleId%3D(?P<articleId>[0-9a-z]+)`),
			Handle:  p.article,
		},
		{
			Keyword: "illund.com/share.html",
			Pattern: parse.Compile(`mainid%3D(?P<mainId>[0-9a-z]+)`),
			Handle:  p.drawing,
		},
	}
}

type fileRef struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

type isoTime struct {
	ISO string `json:"iso"`
}

type user struct {
	Level       int     `json:"level"`
	Nickname    string  `json:"nickname"`
	ObjectID    string  `json:"objectId"`
	CreatedAt   string  `json:"createdAt"`
	HeaderImage fileRef `json:"headerImage"`
	Words       string  `json:"words"`
}

type articleV2 struct {
	DataObject struct {
		Author       user    `json:"author"`
		ContentFile  fileRef `json:"contentFile"`
		PublishDate  isoTime `json:"publishDate"`
		ModifyDate   isoTime `json:"modifyDate"`
		ObjectID     string  `json:"objectId"`
		Title        string  `json:"title"`
		Description  string  `json:"description"`
		ReadCount    int64   `json:"readCount"`
		RewardCoin   int64   `json:"rewardCoin"`
		ThumbUpCount int64   `json:"thumbUpCount"`
		CommentCount int64   `json:"commentCount"`
	} `json:"dataObject"`
	Msg string `json:"msg"`
}

type drawingDetail struct {
	Author       user      `json:"author"`
	CollectCount int64     `json:"collectCount"`
	CommentCount int64     `json:"commentCount"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	Images       []fileRef `json:"images"`
	LikeCount    int64     `json:"likeCount"`
	ReadCount    int64     `json:"readCount"`
	RewardCoin   int64     `json:"rewardCoin"`
	ObjectID     string    `json:"objectId"`
	ModifyDate   isoTime   `json:"modifyDate"`
	PublishDate  isoTime   `json:"publishDate"`
}

func (p *Parser) post(router string, payload, out any) error {
	var got struct {
		Result json.RawMessage `json:"result"`
	}
	if err := p.client.PostJSONDecode(baseURL+router, payload, &got, signHeader(router)); err != nil {
		return err
	}
	return netx.Decode(unwrapResult(got.Result), out)
}

func unwrapResult(raw json.RawMessage) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var inner string
		if err := json.Unmarshal(trimmed, &inner); err == nil {
			return []byte(inner)
		}
	}
	return trimmed
}

func (p *Parser) article(ctx context.Context, m *parse.Match) (*model.Result, error) {
	objectID := m.Get("articleId")
	var got articleV2
	if err := p.post(articleRouter, map[string]string{"articleId": objectID}, &got); err != nil {
		return nil, err
	}
	detail := got.DataObject
	content := p.articleContent(detail.ContentFile)
	if len(content) == 0 {
		if text := htmlx.Text(detail.Description); text != "" {
			content = []model.Content{model.Text{Value: text}}
		}
	}
	return &model.Result{
		Platform:  platform,
		URL:       "https://illund.com/share.html?al=mindlib%3A%2F%2Freactbox%2F%3FarticleId%3D" + objectID + "%26hideTitleBar%3D1%26pagename%3DArticleDetailVC",
		Title:     detail.Title,
		Timestamp: parseISO(detail.PublishDate.ISO),
		Author:    model.Author{Name: detail.Author.Nickname, ID: detail.Author.ObjectID, Avatar: detail.Author.HeaderImage.URL},
		Stats: model.Stats{
			View:    htmlx.Num(detail.ReadCount),
			Like:    htmlx.Num(detail.ThumbUpCount),
			Comment: htmlx.Num(detail.CommentCount),
			Extra:   [][2]string{{"打赏硬币", htmlx.Num(detail.RewardCoin)}},
		},
		Content: content,
	}, nil
}

func (p *Parser) drawing(ctx context.Context, m *parse.Match) (*model.Result, error) {
	objectID := m.Get("mainId")
	var detail drawingDetail
	if err := p.post(drawingRouter, map[string]string{"mainId": objectID}, &detail); err != nil {
		return nil, err
	}
	content := make([]model.Content, 0, len(detail.Images)+1)
	if text := htmlx.Compact(detail.Content); text != "" {
		content = append(content, model.Text{Value: text})
	}
	for _, image := range detail.Images {
		if image.URL != "" {
			content = append(content, model.Image{URL: image.URL})
		}
	}
	return &model.Result{
		Platform:  platform,
		URL:       "https://illund.com/share.html?al=mindlib%3A%2F%2Freactbox%2F%3Fmainid%3D" + objectID + "%26hideTitleBar%3D1%26pagename%3DDrawingDetailVC",
		Title:     detail.Title,
		Timestamp: parseISO(detail.PublishDate.ISO),
		Author:    model.Author{Name: detail.Author.Nickname, ID: detail.Author.ObjectID, Avatar: detail.Author.HeaderImage.URL},
		Stats: model.Stats{
			View:    htmlx.Num(detail.ReadCount),
			Like:    htmlx.Num(detail.LikeCount),
			Collect: htmlx.Num(detail.CollectCount),
			Comment: htmlx.Num(detail.CommentCount),
			Extra:   [][2]string{{"灵感", htmlx.Num(detail.RewardCoin)}},
		},
		Content: content,
	}, nil
}

func (p *Parser) articleContent(file fileRef) []model.Content {
	raw, err := p.client.Get(file.URL, nil)
	if err != nil {
		return nil
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil
	}
	body, err := readHTML(reader, strings.Replace(file.Filename, "_html.zip", "_html.html", 1))
	if err != nil {
		return nil
	}
	lines := splitLines(htmlToText(string(body)))
	if len(lines) > 1 {
		lines = lines[1:]
	}
	out := make([]model.Content, 0, len(lines))
	for _, line := range lines {
		out = append(out, model.Text{Value: line})
	}
	return out
}

func readHTML(reader *zip.Reader, name string) ([]byte, error) {
	var fallback *zip.File
	for _, entry := range reader.File {
		if entry.Name == name {
			return readEntry(entry)
		}
		if fallback == nil && strings.HasSuffix(strings.ToLower(entry.Name), ".html") {
			fallback = entry
		}
	}
	if fallback == nil {
		return nil, errorString("ILLU 正文压缩包没有 html")
	}
	return readEntry(fallback)
}

func readEntry(entry *zip.File) ([]byte, error) {
	f, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var newlineTags = map[string]bool{
	"p": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "li": true, "blockquote": true, "pre": true, "hr": true,
}

func htmlToText(raw string) string {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return htmlx.Text(raw)
	}
	var buf strings.Builder
	walkText(root, &buf)
	return strings.TrimSpace(buf.String())
}

func walkText(n *html.Node, buf *strings.Builder) {
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
				walkText(c, buf)
				continue
			}
			if name == "a" {
				if text := anchorText(c, "https://illund.com/"); text != "" {
					buf.WriteString(text)
					continue
				}
			}
			walkText(c, buf)
		}
	}
}

func anchorText(n *html.Node, base string) string {
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

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func splitLines(text string) []string {
	out := make([]string, 0, 8)
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func parseISO(value string) int64 {
	if parsed, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return parsed.Unix()
	}
	return htmlx.Time(value)
}

func signHeader(path string) map[string]string {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := randomNonce()
	return map[string]string{
		"X-Bmob-SDK-Type":       "API",
		"X-Bmob-Safe-Timestamp": timestamp,
		"X-Bmob-Noncestr-Key":   nonce,
		"X-Bmob-Safe-Sign":      md5Hex(path + timestamp + "350704" + nonce),
		"X-Bmob-Secret-Key":     secretKey,
	}
}

func randomNonce() string {
	var b strings.Builder
	for i := 0; i < 16; i++ {
		b.WriteByte(nonceChars[rand.IntN(len(nonceChars))])
	}
	return b.String()
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
