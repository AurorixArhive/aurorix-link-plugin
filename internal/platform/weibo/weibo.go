package weibo

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
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
	"Referer":    "https://www.weibo.com/",
}

var mediaHeaders = map[string]string{"Referer": "https://weibo.com/"}

var trailingURL = regexp.MustCompile(" http://t\\.cn/\\S+\u200b$")

var platform = model.Platform{Name: "weibo", DisplayName: "微博"}

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
			Keyword: "weibo.com/tv",
			Pattern: parse.Compile(`weibo\.com/tv/show/\d{4}:\d+[^\s]*`),
			Params:  map[string]parse.ParamRule{"mid": {AsInt: true}},
			Handle:  p.tvShow,
		},
		{
			Keyword: "video.weibo.com/show",
			Pattern: parse.Domain("video.weibo.com/show"),
			Params:  map[string]parse.ParamRule{"fid": {}},
			Handle:  p.videoShow,
		},
		{
			Keyword: "m.weibo.cn",
			Pattern: parse.Compile(`weibo\.cn/(?:status|detail|\d+)/(?P<wid>[0-9a-zA-Z]+)`),
			Handle:  p.status,
		},
		{
			Keyword: "weibo.com",
			Pattern: parse.Compile(`weibo\.com/\d+/(?P<wid>[0-9a-zA-Z]+)`),
			Handle:  p.status,
		},
		{
			Keyword: "mapp.api.weibo",
			Pattern: parse.Compile(`mapp\.api\.weibo\.cn/fx/[0-9A-Za-z]+\.html`),
			Handle:  p.mapp,
		},
		{
			Keyword: "weibo.com/ttarticle",
			Pattern: parse.Compile(`id=(?P<id>\d+)`),
			Handle:  p.article,
		},
		{
			Keyword: "weibo.com/article",
			Pattern: parse.Compile(`/id/(?P<id>\d+)`),
			Handle:  p.article,
		},
	}
}

type weiboUser struct {
	IDStr           string `json:"idstr"`
	ScreenName      string `json:"screen_name"`
	ProfileImageURL string `json:"profile_image_url"`
}

type weiboPic struct {
	Original struct {
		URL string `json:"url"`
	} `json:"original"`
	PicID string `json:"pic_id"`
	Type  string `json:"type"`
	Video string `json:"video"`
}

type weiboMediaInfo struct {
	Name        string `json:"name"`
	StreamURLHD string `json:"stream_url_hd"`
}

type weiboPageInfo struct {
	PagePic   string          `json:"page_pic"`
	PageTitle string          `json:"page_title"`
	PageURL   string          `json:"page_url"`
	MediaInfo *weiboMediaInfo `json:"media_info"`
}

type weiboData struct {
	User            weiboUser           `json:"user"`
	TextRaw         string              `json:"text_raw"`
	IDStr           string              `json:"idstr"`
	CreatedAt       string              `json:"created_at"`
	RepostsCount    int64               `json:"reposts_count"`
	CommentsCount   int64               `json:"comments_count"`
	AttitudesCount  int64               `json:"attitudes_count"`
	IsLongText      bool                `json:"isLongText"`
	PicNum          int64               `json:"pic_num"`
	RegionName      string              `json:"region_name"`
	PicInfos        map[string]weiboPic `json:"pic_infos"`
	PageInfo        *weiboPageInfo      `json:"page_info"`
	RetweetedStatus *weiboData          `json:"retweeted_status"`
}

type longText struct {
	Data struct {
		LongTextContent string `json:"longTextContent"`
	} `json:"data"`
}

type playInfo struct {
	Title          string            `json:"title"`
	Text           string            `json:"text"`
	Author         string            `json:"author"`
	AuthorID       int64             `json:"author_id"`
	Avatar         string            `json:"avatar"`
	CoverImage     string            `json:"cover_image"`
	StreamURL      string            `json:"stream_url"`
	RealDate       int64             `json:"real_date"`
	URLs           map[string]string `json:"urls"`
	DurationTime   float64           `json:"duration_time"`
	AttitudesCount int64             `json:"attitudes_count"`
	CommentsCount  string            `json:"comments_count"`
	RepostsCount   string            `json:"reposts_count"`
	PlayCount      string            `json:"play_count"`
	IPInfoStr      string            `json:"ip_info_str"`
}

type showWrapper struct {
	Data struct {
		PlayInfo playInfo `json:"Component_Play_Playinfo"`
	} `json:"data"`
}

type articleDetail struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		URL      string `json:"url"`
		Title    string `json:"title"`
		HTML     string `json:"content"`
		UserInfo struct {
			ID              int64  `json:"id"`
			ScreenName      string `json:"screen_name"`
			ProfileImageURL string `json:"profile_image_url"`
		} `json:"userinfo"`
		CreateAtUnix int64  `json:"create_at_unix"`
		ReadCount    string `json:"read_count"`
		RegionInfo   struct {
			RegionName string `json:"region_name"`
		} `json:"region_info"`
	} `json:"data"`
}

func (p *Parser) requestHeaders(extra map[string]string) map[string]string {
	merged := make(map[string]string, len(headers)+len(extra)+1)
	for k, v := range headers {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	if p.cookie != "" {
		merged["Cookie"] = p.cookie
	}
	return merged
}

func (p *Parser) get(rawURL string) ([]byte, error) {
	return p.client.Get(rawURL, p.requestHeaders(nil))
}

func (p *Parser) tvShow(ctx context.Context, m *parse.Match) (*model.Result, error) {
	return p.byID(ctx, midToID(m.Get("mid")))
}

func (p *Parser) videoShow(ctx context.Context, m *parse.Match) (*model.Result, error) {
	return p.show(ctx, m.Get("fid"))
}

func (p *Parser) status(ctx context.Context, m *parse.Match) (*model.Result, error) {
	return p.byID(ctx, m.Get("wid"))
}

func (p *Parser) mapp(ctx context.Context, m *parse.Match) (*model.Result, error) {
	target := m.URL
	if !strings.HasPrefix(target, "http") {
		target = "https://" + target
	}
	final, err := p.client.FinalURL(target, p.requestHeaders(nil))
	if err != nil {
		return nil, err
	}
	if final == target {
		return nil, errNoRedirect
	}
	for _, route := range p.Routes() {
		if route.Keyword == "mapp.api.weibo" {
			continue
		}
		if match := matchRoute(route, final); match != nil {
			return route.Handle(ctx, match)
		}
	}
	return nil, errNoRoute
}

func (p *Parser) byID(ctx context.Context, id string) (*model.Result, error) {
	raw, err := p.get("https://www.weibo.com/ajax/statuses/show?id=" + url.QueryEscape(id))
	if err != nil {
		return nil, err
	}
	if isHTML(raw) {
		return nil, errNeedCookie
	}
	var data weiboData
	if err := netx.Decode(raw, &data); err != nil {
		return nil, err
	}
	if data.IDStr == "" && data.User.ScreenName == "" {
		return nil, errEmptyStatus
	}
	return p.build(ctx, &data)
}

func (p *Parser) build(ctx context.Context, data *weiboData) (*model.Result, error) {
	var repost *model.Result
	if data.RetweetedStatus != nil {
		if r, err := p.build(ctx, data.RetweetedStatus); err == nil {
			repost = r
		}
	}
	content, err := p.content(ctx, data)
	if err != nil {
		return nil, err
	}
	return &model.Result{
		Platform: platform,
		URL:      "https://weibo.com/" + data.User.IDStr + "/" + data.IDStr,
		Author: model.Author{
			Name:   data.User.ScreenName,
			ID:     data.User.IDStr,
			Avatar: data.User.ProfileImageURL,
			Desc:   data.RegionName,
		},
		Stats: model.Stats{
			Like:    htmlx.Num(data.AttitudesCount),
			Share:   htmlx.Num(data.RepostsCount),
			Comment: htmlx.Num(data.CommentsCount),
		},
		Timestamp: weiboTime(data.CreatedAt),
		Content:   content,
		Repost:    repost,
	}, nil
}

func (p *Parser) content(ctx context.Context, data *weiboData) ([]model.Content, error) {
	text := data.TextRaw
	if data.IsLongText {
		raw, err := p.get("https://m.weibo.cn/statuses/extend?id=" + url.QueryEscape(data.IDStr))
		if err != nil {
			return nil, err
		}
		var got longText
		if err := netx.Decode(raw, &got); err != nil {
			return nil, err
		}
		text = longHTML(got.Data.LongTextContent)
	}
	text = strings.TrimSpace(trailingURL.ReplaceAllString(text, ""))

	out := make([]model.Content, 0, len(data.PicInfos)+2)
	if text != "" {
		out = append(out, model.Text{Value: text})
	}
	keys := make([]string, 0, len(data.PicInfos))
	for key := range data.PicInfos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pic := data.PicInfos[key]
		if pic.Type == "livephoto" {
			out = append(out, model.LivePhoto{Image: pic.Original.URL, Stream: pic.Video, Headers: mediaHeaders})
			continue
		}
		out = append(out, model.Image{URL: pic.Original.URL})
	}
	if data.PageInfo != nil && data.PageInfo.MediaInfo != nil {
		out = append(out, model.Video{
			URL:     data.PageInfo.MediaInfo.StreamURLHD,
			Cover:   data.PageInfo.PagePic,
			Headers: mediaHeaders,
		})
	}
	return out, nil
}

func (p *Parser) show(ctx context.Context, fid string) (*model.Result, error) {
	payload, err := json.Marshal(map[string]any{"Component_Play_Playinfo": map[string]any{"oid": fid}})
	if err != nil {
		return nil, err
	}
	headers := p.requestHeaders(map[string]string{"Referer": "https://weibo.com/"})
	raw, err := p.client.PostForm("https://weibo.com/tv/api/component?page=/show/"+fid,
		map[string][]string{"data": {string(payload)}}, headers)
	if err != nil {
		return nil, err
	}
	if isHTML(raw) {
		return nil, errNeedCookie
	}
	var got showWrapper
	if err := netx.Decode(raw, &got); err != nil {
		return nil, err
	}
	info := got.Data.PlayInfo

	content := make([]model.Content, 0, 2)
	if desc := htmlx.Text(info.Text); desc != "" {
		content = append(content, model.Text{Value: desc})
	}
	content = append(content, model.Video{
		URL:      info.videoURL(),
		Cover:    httpsURL(info.CoverImage),
		Duration: info.DurationTime,
		Headers:  mediaHeaders,
	})

	return &model.Result{
		Platform: platform,
		URL:      "https://h5.video.weibo.com/show/" + fid,
		Title:    info.Title,
		Author: model.Author{
			Name:   info.Author,
			ID:     strconv.FormatInt(info.AuthorID, 10),
			Avatar: httpsURL(info.Avatar),
			Desc:   info.IPInfoStr,
		},
		Stats: model.Stats{
			View:    info.PlayCount,
			Like:    htmlx.Num(info.AttitudesCount),
			Comment: info.CommentsCount,
			Share:   info.RepostsCount,
		},
		Timestamp: info.RealDate,
		Content:   content,
	}, nil
}

func (p *Parser) article(ctx context.Context, m *parse.Match) (*model.Result, error) {
	id := m.Get("id")
	raw, err := p.get("https://card.weibo.com/article/m/aj/detail?id=" + url.QueryEscape(id))
	if err != nil {
		return nil, err
	}
	var got articleDetail
	if err := netx.Decode(raw, &got); err != nil {
		return nil, err
	}
	data := got.Data
	return &model.Result{
		Platform: platform,
		URL:      data.URL,
		Title:    data.Title,
		Author: model.Author{
			Name:   data.UserInfo.ScreenName,
			ID:     strconv.FormatInt(data.UserInfo.ID, 10),
			Avatar: data.UserInfo.ProfileImageURL,
			Desc:   data.RegionInfo.RegionName,
		},
		Stats:     model.Stats{View: data.ReadCount},
		Timestamp: data.CreateAtUnix,
		Content:   articleContent(data.HTML),
	}, nil
}

func (pi playInfo) videoURL() string {
	for _, value := range pi.URLs {
		if value != "" {
			return httpsURL(value)
		}
	}
	return pi.StreamURL
}

func httpsURL(value string) string {
	if value == "" || strings.HasPrefix(value, "http") {
		return value
	}
	return "https:" + value
}

func isHTML(raw []byte) bool {
	text := strings.TrimSpace(string(raw))
	return strings.HasPrefix(text, "<") || strings.Contains(text, "passport.weibo")
}

func weiboTime(value string) int64 {
	for _, layout := range []string{
		"Mon Jan _2 15:04:05 -0700 2006",
		"Mon Jan 02 15:04:05 -0700 2006",
	} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed.Unix()
		}
	}
	return 0
}

var newlineTags = map[string]bool{
	"p": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "li": true, "blockquote": true, "pre": true, "hr": true,
}

func articleContent(raw string) []model.Content {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	out := make([]model.Content, 0, 8)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p":
				if text := articleText(n); text != "" {
					out = append(out, model.Text{Value: text})
				}
			case "img":
				if src := attr(n, "src"); src != "" {
					out = append(out, model.Image{URL: src})
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out
}

func articleText(n *html.Node) string {
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.ElementNode {
			if newlineTags[cur.Data] {
				buf.WriteString("\n")
			}
			if cur.Data == "a" {
				if text := anchorText(cur, "https://weibo.com/"); text != "" {
					buf.WriteString(text)
				}
				return
			}
		}
		if cur.Type == html.TextNode {
			if text := htmlx.Compact(cur.Data); text != "" {
				buf.WriteString(text)
			}
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.TrimSpace(buf.String())
}

var cardIconRe = regexp.MustCompile(`timeline_card_small_([a-z0-9]+)`)

var entityLabels = map[string]string{"super": "超话"}

func longHTML(raw string) string {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return htmlx.Compact(raw)
	}
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.CommentNode:
			return
		case html.TextNode:
			if text := htmlx.Compact(n.Data); text != "" {
				buf.WriteString(text)
			}
			return
		case html.ElementNode:
		}
		if newlineTags[n.Data] {
			buf.WriteString("\n")
			if n.Data == "br" {
				return
			}
		}
		if n.Data == "img" {
			if alt := attr(n, "alt"); alt != "" {
				buf.WriteString(alt)
			}
			return
		}
		if n.Data == "a" {
			buf.WriteString(longAnchor(n))
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return buf.String()
}

func longAnchor(n *html.Node) string {
	if surl := findClass(n, "surl-text"); surl != nil {
		title := nodeText(surl)
		if label := entityLabel(n); title != "" && label != "" {
			return "#" + title + "[" + label + "]#"
		}
		if dataURL := attr(n, "data-url"); strings.HasPrefix(dataURL, "http://t.cn/") || strings.HasPrefix(dataURL, "https://t.cn/") {
			return dataURL
		}
		if title != "" {
			return title
		}
		return nodeText(n)
	}
	label := nodeText(n)
	href := attr(n, "href")
	if href != "" && label != "" && label != href {
		return label + " (" + href + ")"
	}
	if label != "" {
		return label
	}
	return href
}

func entityLabel(n *html.Node) string {
	var found string
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if found != "" {
			return
		}
		if cur.Type == html.ElementNode && cur.Data == "img" {
			if match := cardIconRe.FindStringSubmatch(attr(cur, "src")); match != nil {
				found = entityLabels[strings.ToLower(match[1])]
				return
			}
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
}

func findClass(n *html.Node, class string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if found != nil {
			return
		}
		if cur.Type == html.ElementNode && cur.Data == "span" {
			for _, item := range strings.Fields(attr(cur, "class")) {
				if item == class {
					found = cur
					return
				}
			}
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
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

func matchRoute(route parse.Route, raw string) *parse.Match {
	groups := route.Pattern.FindStringSubmatch(raw)
	if groups == nil {
		return nil
	}
	params := map[string]string{}
	for i, name := range route.Pattern.SubexpNames() {
		if i > 0 && i < len(groups) && name != "" && groups[i] != "" {
			params[name] = groups[i]
		}
	}
	if parsed, err := url.Parse(groups[0]); err == nil {
		for key, values := range parsed.Query() {
			if len(values) > 0 && params[key] == "" {
				params[key] = values[0]
			}
		}
		if i := strings.IndexByte(parsed.Fragment, '?'); i >= 0 {
			if values, err := url.ParseQuery(parsed.Fragment[i+1:]); err == nil {
				for key, list := range values {
					if len(list) > 0 && params[key] == "" {
						params[key] = list[0]
					}
				}
			}
		}
	}
	for name, rule := range route.Params {
		value, has := params[name]
		if !has && rule.Default != "" {
			value, has = rule.Default, true
			params[name] = value
		}
		if !has {
			if rule.Optional {
				continue
			}
			return nil
		}
		if rule.Equals != "" && value != rule.Equals {
			return nil
		}
		if len(rule.OneOf) > 0 && !contains(rule.OneOf, value) {
			return nil
		}
		if rule.AsInt {
			if _, err := strconv.Atoi(value); err != nil {
				return nil
			}
		}
	}
	return &parse.Match{URL: groups[0], Params: params}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

const midAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func midToID(mid string) string {
	runes := []byte(mid)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	size := (len(runes) + 6) / 7
	result := make([]string, 0, size)
	for i := 0; i < size; i++ {
		end := (i + 1) * 7
		if end > len(runes) {
			end = len(runes)
		}
		chunk := reverse(string(runes[i*7 : end]))
		parts := base62(chunk)
		if i < size-1 && len(parts) < 4 {
			parts = strings.Repeat("0", 4-len(parts)) + parts
		}
		result = append(result, parts)
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return strings.Join(result, "")
}

func base62(chunk string) string {
	number, err := strconv.Atoi(chunk)
	if err != nil {
		return chunk
	}
	if number == 0 {
		return "0"
	}
	out := ""
	for number > 0 {
		out = string(midAlphabet[number%62]) + out
		number /= 62
	}
	return out
}

func reverse(value string) string {
	runes := []byte(value)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

var (
	errEmptyStatus = errorString("微博接口没有返回正文")
	errNeedCookie  = errorString("微博接口要求登录态, 请在配置里填微博 Cookie")
	errNoRedirect  = errorString("微博短链没有跳转到微博页")
	errNoRoute     = errorString("微博短链没有解析出可用地址")
)

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
