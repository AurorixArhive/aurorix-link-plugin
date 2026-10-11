package zhihu

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://www.zhihu.com/",
}

var videoHeaders = map[string]string{
	"User-Agent": headers["User-Agent"],
	"x-app-za":   "OS=webplayer",
	"x-referer":  "",
}

var platform = model.Platform{Name: "zhihu", DisplayName: "知乎"}

type Parser struct {
	client *netx.Client
	dc0    string
}

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) SetCookie(value string) {
	p.dc0 = cookieField(value, "d_c0")
	p.client.SetCookie(value)
}

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "zhuanlan.zhihu.com/p",
			Pattern: parse.Compile(`zhuanlan\.zhihu\.com/p/(?P<article_id>\d+)`),
			Handle:  p.article,
		},
		{
			Keyword: "www.zhihu.com/question",
			Pattern: parse.Compile(`www\.zhihu\.com/question/\d+/answer/(?P<answer_id>\d+)`),
			Handle:  p.answer,
		},
		{
			Keyword: "www.zhihu.com/question",
			Pattern: parse.Compile(`www\.zhihu\.com/question/(?P<question_id>\d+)$`),
			Handle:  p.question,
		},
	}
}

func (p *Parser) get(rawURL string, out any, extra map[string]string) error {
	head := signHeaders(rawURL, p.dc0)
	for key, value := range extra {
		head[key] = value
	}
	return p.client.GetJSON(rawURL, out, head)
}

type author struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	URLToken  string `json:"url_token"`
	Headline  string `json:"headline"`
}

type statistics struct {
	CommentCount  int64 `json:"comment_count"`
	DownVoteCount int64 `json:"down_vote_count"`
	Favorites     int64 `json:"favorites"`
	LikeCount     int64 `json:"like_count"`
	UpVoteCount   int64 `json:"up_vote_count"`
}

type reaction struct {
	Statistics statistics `json:"statistics"`
}

type articleData struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Author   author   `json:"author"`
	Created  int64    `json:"created"`
	Updated  int64    `json:"updated"`
	IPInfo   string   `json:"ip_info"`
	Reaction reaction `json:"reaction"`
}

type answerData struct {
	ID          string   `json:"id"`
	Content     string   `json:"content"`
	Author      author   `json:"author"`
	CreatedTime int64    `json:"created_time"`
	UpdatedTime int64    `json:"updated_time"`
	IPInfo      string   `json:"ip_info"`
	Reaction    reaction `json:"reaction"`
	Question    struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"question"`
}

type questionData struct {
	Title         string `json:"title"`
	Detail        string `json:"detail"`
	UpdatedTime   int64  `json:"updated_time"`
	VisitCount    int64  `json:"visit_count"`
	CommentCount  int64  `json:"comment_count"`
	FollowerCount int64  `json:"follower_count"`
	VoteupCount   int64  `json:"voteup_count"`
	Author        author `json:"author"`
}

func (p *Parser) article(ctx context.Context, m *parse.Match) (*model.Result, error) {
	id := m.Get("article_id")
	target := "https://www.zhihu.com/api/v4/articles/" + id +
		"?include=content,topics,paid_info,can_comment,excerpt,thanks_count,voteup_count,comment_count,visited_count,relationship,ip_info,relationship.vote,author.badge_v2"
	var got articleData
	if err := p.get(target, &got, nil); err != nil {
		return nil, err
	}
	content, err := p.rich(got.Content, "article")
	if err != nil {
		return nil, err
	}
	stats := got.Reaction.Statistics
	return &model.Result{
		Platform:  platform,
		URL:       "https://zhuanlan.zhihu.com/p/" + got.ID,
		Title:     got.Title,
		Timestamp: got.Updated,
		Author:    makeAuthor(got.Author),
		Stats: model.Stats{
			Like:    htmlx.Num(stats.LikeCount),
			Comment: htmlx.Num(stats.CommentCount),
			Collect: htmlx.Num(stats.Favorites),
			Extra: [][2]string{
				{"反对", htmlx.Num(stats.DownVoteCount)},
				{"赞成", htmlx.Num(stats.UpVoteCount)},
			},
		},
		Content: content,
	}, nil
}

func (p *Parser) answer(ctx context.Context, m *parse.Match) (*model.Result, error) {
	id := m.Get("answer_id")
	target := "https://www.zhihu.com/api/v4/answers/" + id +
		"?include=content,paid_info,can_comment,excerpt,thanks_count,voteup_count,comment_count,visited_count,attachment,reaction,ip_info,pagination_info,question.topics,reaction.relation.voting,author.badge_v2"
	var got answerData
	if err := p.get(target, &got, nil); err != nil {
		return nil, err
	}
	content, err := p.rich(got.Content, "answer")
	if err != nil {
		return nil, err
	}
	stats := got.Reaction.Statistics
	return &model.Result{
		Platform:  platform,
		URL:       "https://www.zhihu.com/question/" + got.Question.ID + "/answer/" + id,
		Title:     got.Question.Title,
		Timestamp: got.UpdatedTime,
		Author:    makeAuthor(got.Author),
		Stats: model.Stats{
			Like:    htmlx.Num(stats.LikeCount),
			Comment: htmlx.Num(stats.CommentCount),
			Collect: htmlx.Num(stats.Favorites),
			Extra: [][2]string{
				{"反对", htmlx.Num(stats.DownVoteCount)},
				{"赞成", htmlx.Num(stats.UpVoteCount)},
			},
		},
		Content: content,
	}, nil
}

func (p *Parser) question(ctx context.Context, m *parse.Match) (*model.Result, error) {
	id := m.Get("question_id")
	target := "https://www.zhihu.com/api/v4/questions/" + id +
		"?include=read_count,visit_count,answer_count,voteup_count,comment_count,follower_count,detail,excerpt,author,relationship.is_following,topics"
	var got questionData
	if err := p.get(target, &got, nil); err != nil {
		return nil, err
	}
	content, err := p.rich(got.Detail, "question")
	if err != nil {
		return nil, err
	}
	return &model.Result{
		Platform:  platform,
		URL:       "https://www.zhihu.com/question/" + id,
		Title:     got.Title,
		Timestamp: got.UpdatedTime,
		Author:    makeAuthor(got.Author),
		Stats: model.Stats{
			View:    htmlx.Num(got.VisitCount),
			Comment: htmlx.Num(got.CommentCount),
			Extra: [][2]string{
				{"赞成", htmlx.Num(got.VoteupCount)},
				{"关注", htmlx.Num(got.FollowerCount)},
			},
		},
		Content: content,
	}, nil
}

func makeAuthor(a author) model.Author {
	return model.Author{Name: a.Name, ID: a.URLToken, Avatar: a.AvatarURL, Desc: a.Headline}
}

var newlineTags = map[string]bool{
	"p": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "li": true, "blockquote": true, "pre": true, "hr": true,
}

type builder struct {
	parser      *Parser
	contentType string
	out         []model.Content
	buf         strings.Builder
}

func (p *Parser) rich(raw, contentType string) ([]model.Content, error) {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return []model.Content{model.Text{Value: htmlx.Text(raw)}}, nil
	}
	b := &builder{parser: p, contentType: contentType}
	if err := b.walk(root); err != nil {
		return nil, err
	}
	b.flush()
	return b.out, nil
}

func (b *builder) flush() {
	if text := strings.TrimSpace(b.buf.String()); text != "" {
		b.out = append(b.out, model.Text{Value: text})
	}
	b.buf.Reset()
}

func (b *builder) walk(n *html.Node) error {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			if text := htmlx.Compact(c.Data); text != "" {
				b.buf.WriteString(text)
			}
		case html.ElementNode:
			if err := b.element(c); err != nil {
				return err
			}
		default:
			if err := b.walk(c); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *builder) element(c *html.Node) error {
	name := strings.ToLower(c.Data)
	if newlineTags[name] {
		b.buf.WriteString("\n")
		return b.walk(c)
	}
	if name == "a" && hasClass(c, "video-box") {
		video, err := b.parser.video(c, b.contentType)
		if err != nil {
			return err
		}
		if video != nil {
			b.flush()
			b.out = append(b.out, *video)
		}
		if label := strings.TrimSpace(attr(c, "data-name")); label != "" {
			b.buf.WriteString(label)
		}
		return nil
	}
	if name == "a" && !hasTag(c, "img") {
		if text := anchorText(c, "https://www.zhihu.com/"); text != "" {
			b.buf.WriteString(text)
			return nil
		}
	}
	if name == "img" {
		if src := imgURL(c); src != "" {
			b.flush()
			b.out = append(b.out, model.Image{URL: src})
		}
		return nil
	}
	return b.walk(c)
}

type mp4Item struct {
	Quality  string   `json:"quality"`
	URL      []string `json:"url"`
	Duration float64  `json:"duration"`
}

type playInfo struct {
	VideoPlay struct {
		Playlist struct {
			MP4 []mp4Item `json:"mp4"`
		} `json:"playlist"`
		DefaultCover string `json:"default_cover"`
	} `json:"video_play"`
}

func (p *Parser) video(tag *html.Node, contentType string) (*model.Video, error) {
	videoID := attr(tag, "data-lens-id")
	if videoID == "" {
		return nil, nil
	}
	var got playInfo
	if err := p.client.PostJSONDecode("https://www.zhihu.com/api/v4/video/play_info", map[string]any{
		"content_id":       videoID,
		"video_id":         videoID,
		"content_type_str": contentType,
		"is_only_video":    true,
		"scene_code":       "answer_detail_web",
	}, &got, videoHeaders); err != nil {
		return nil, err
	}
	list := got.VideoPlay.Playlist.MP4
	if len(list) == 0 {
		return nil, nil
	}
	sort.SliceStable(list, func(i, j int) bool { return qualityRank(list[i].Quality) > qualityRank(list[j].Quality) })
	best := list[0]
	if len(best.URL) == 0 {
		return nil, nil
	}
	return &model.Video{
		URL:      best.URL[0],
		Cover:    got.VideoPlay.DefaultCover,
		Duration: best.Duration,
		Headers:  videoHeaders,
	}, nil
}

func qualityRank(quality string) int {
	switch strings.ToUpper(quality) {
	case "FHD":
		return 3
	case "HD":
		return 2
	case "SD":
		return 1
	}
	return 0
}

func imgURL(n *html.Node) string {
	for _, key := range []string{"data-original", "data-actualsrc", "data-default-watermark-src", "src"} {
		if value := attr(n, key); value != "" {
			return value
		}
	}
	return ""
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

func hasTag(n *html.Node, name string) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, name) {
			return true
		}
		if hasTag(c, name) {
			return true
		}
	}
	return false
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

func cookieField(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

const (
	zse93    = "101_3_3.0"
	alphabet = "6fpLRqJO8M/c3jnYxFkUVC4ZIG12SiH=5v0mXDazWBTsuw7QetbKdoPyAl+hN9rgE"
)

var key16 = []byte("059053f7d15e01d7")

var zk = [32]uint32{
	1170614578, 1024848638, 1413669199, 3951632832, 3528873006, 2921909214, 4151847688, 3997739139,
	1933479194, 3323781115, 3888513386, 460404854, 3747539722, 2403641034, 2615871395, 2119585428,
	2265697227, 2035090028, 2773447226, 4289380121, 4217216195, 2200601443, 3051914490, 1579901135,
	1321810770, 456816404, 2903323407, 4065664991, 330002838, 3506006750, 363569021, 2347096187,
}

var zb = [256]uint32{
	20, 223, 245, 7, 248, 2, 194, 209,
	87, 6, 227, 253, 240, 128, 222, 91,
	237, 9, 125, 157, 230, 93, 252, 205,
	90, 79, 144, 199, 159, 197, 186, 167,
	39, 37, 156, 198, 38, 42, 43, 168,
	217, 153, 15, 103, 80, 189, 71, 191,
	97, 84, 247, 95, 36, 69, 14, 35,
	12, 171, 28, 114, 178, 148, 86, 182,
	32, 83, 158, 109, 22, 255, 94, 238,
	151, 85, 77, 124, 254, 18, 4, 26,
	123, 176, 232, 193, 131, 172, 143, 142,
	150, 30, 10, 146, 162, 62, 224, 218,
	196, 229, 1, 192, 213, 27, 110, 56,
	231, 180, 138, 107, 242, 187, 54, 120,
	19, 44, 117, 228, 215, 203, 53, 239,
	251, 127, 81, 11, 133, 96, 204, 132,
	41, 115, 73, 55, 249, 147, 102, 48,
	122, 145, 106, 118, 74, 190, 29, 16,
	174, 5, 177, 129, 63, 113, 99, 31,
	161, 76, 246, 34, 211, 13, 60, 68,
	207, 160, 65, 111, 82, 165, 67, 169,
	225, 57, 112, 244, 155, 51, 236, 200,
	233, 58, 61, 47, 100, 137, 185, 64,
	17, 70, 234, 163, 219, 108, 170, 166,
	59, 149, 52, 105, 24, 212, 78, 173,
	45, 0, 116, 226, 119, 136, 206, 135,
	175, 195, 25, 92, 121, 208, 126, 139,
	3, 75, 141, 21, 130, 98, 241, 40,
	154, 66, 184, 49, 181, 46, 243, 88,
	101, 183, 8, 23, 72, 188, 104, 179,
	210, 134, 250, 201, 164, 89, 216, 202,
	220, 50, 221, 152, 140, 33, 235, 214,
}

func signHeaders(rawURL, dc0 string) map[string]string {
	return map[string]string{
		"x-zse-93":         zse93,
		"x-zse-96":         "2.0_" + encryptZseV4(zseSource(rawURL, dc0)),
		"x-requested-with": "fetch",
	}
}

func zseSource(rawURL, dc0 string) string {
	path := rawURL
	if parsed, err := url.Parse(rawURL); err == nil {
		path = parsed.Path
		if parsed.RawQuery != "" {
			path += "?" + parsed.RawQuery
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return md5Hex(zse93 + "+" + path + "+" + dc0)
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func encryptZseV4(input string) string {
	plain := []byte{210, 0}
	plain = append(plain, []byte(percentQuote(input))...)
	pad := 16 - len(plain)%16
	for i := 0; i < pad; i++ {
		plain = append(plain, byte(pad))
	}
	first := make([]byte, 16)
	for i := 0; i < 16; i++ {
		first[i] = plain[i] ^ key16[i] ^ 42
	}
	c0 := rBlock(first)
	cipher := make([]byte, len(plain))
	copy(cipher, c0)
	if len(plain) > 16 {
		copy(cipher[16:], xBlocks(plain[16:], c0))
	}
	return customEncode(cipher)
}

func percentQuote(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.-~()*!", r) {
			b.WriteRune(r)
			continue
		}
		for _, c := range []byte(string(r)) {
			b.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return b.String()
}

func rBlock(input []byte) []byte {
	tr := [36]uint32{}
	for i := 0; i < 4; i++ {
		tr[i] = readU32(input[i*4:])
	}
	for i := 0; i < 32; i++ {
		ta := gTransform(tr[i+1] ^ tr[i+2] ^ tr[i+3] ^ zk[i])
		tr[i+4] = tr[i] ^ ta
	}
	out := make([]byte, 16)
	writeU32(tr[35], out[0:])
	writeU32(tr[34], out[4:])
	writeU32(tr[33], out[8:])
	writeU32(tr[32], out[12:])
	return out
}

func gTransform(tt uint32) uint32 {
	ti := (zb[(tt>>24)&0xFF]&0xFF)<<24 |
		(zb[(tt>>16)&0xFF]&0xFF)<<16 |
		(zb[(tt>>8)&0xFF]&0xFF)<<8 |
		(zb[tt&0xFF] & 0xFF)
	return ti ^ rotl(ti, 2) ^ rotl(ti, 10) ^ rotl(ti, 18) ^ rotl(ti, 24)
}

func rotl(value uint32, bits uint) uint32 { return value<<bits | value>>(32-bits) }

func xBlocks(data, iv0 []byte) []byte {
	iv := make([]byte, 16)
	copy(iv, iv0)
	out := make([]byte, 0, len(data))
	for off := 0; off < len(data); off += 16 {
		mixed := make([]byte, 16)
		for i := 0; i < 16; i++ {
			mixed[i] = data[off+i] ^ iv[i]
		}
		iv = rBlock(mixed)
		out = append(out, iv...)
	}
	return out
}

func customEncode(input []byte) string {
	raw := input
	if rem := len(raw) % 3; rem != 0 {
		raw = append(append([]byte{}, raw...), make([]byte, 3-rem)...)
	}
	var b strings.Builder
	i := 0
	for p := len(raw) - 1; p >= 0; p -= 3 {
		v := int(raw[p] ^ byte((58>>(8*(i%4)))&0xFF))
		i++
		v |= int(raw[p-1]^byte((58>>(8*(i%4)))&0xFF)) << 8
		i++
		v |= int(raw[p-2]^byte((58>>(8*(i%4)))&0xFF)) << 16
		i++
		b.WriteByte(alphabet[v&63])
		b.WriteByte(alphabet[(v>>6)&63])
		b.WriteByte(alphabet[(v>>12)&63])
		b.WriteByte(alphabet[(v>>18)&63])
	}
	return b.String()
}

func readU32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func writeU32(v uint32, b []byte) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

func init() { parse.Register(New) }
