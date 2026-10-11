package heybox

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://www.xiaoheihe.cn/",
	"Origin":     "https://www.xiaoheihe.cn",
	"Accept":     "application/json, text/plain, */*",
}

const (
	heyboxHost  = "api.xiaoheihe.cn"
	heyboxPath  = "/bbs/app/link/tree"
	tokenAPI    = "https://fp-it.portal101.cn/deviceprofile/v4"
	smOrganize  = "0yD85BjYvGFAvHaSQ1mc"
	smAppID     = "heybox_website"
	smPublicKey = "MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCXj9exmI4nQjmT52iwr+yf7hAQ06bfSZHTAHUfRBYiagCf/whhd8es0R79wBigpiHLd28TKA8b8mGR8OiiI1hV+qfynCWihvp3mdj8MiiH6SU3lhro2hkfYzImZB0RmWr2zE4Xt1+A6Oyp6bf+W7JSxYUXHw3nNv7Td4jw4jEFKQIDAQAB"
	heyboxUA    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0"
)

var platform = model.Platform{Name: "heybox", DisplayName: "小黑盒"}

type Parser struct {
	client *netx.Client
	token  string
}

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) SetCookie(value string) { p.token = strings.TrimSpace(value) }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "api.xiaoheihe.cn/v3/bbs/app/api/web/share",
			Pattern: parse.Domain("api.xiaoheihe.cn/v3/bbs/app/api/web/share"),
			Params:  map[string]parse.ParamRule{"link_id": {}},
			Handle:  p.link,
		},
		{
			Keyword: "xiaoheihe.cn/bbs/post_share",
			Pattern: parse.Domain("xiaoheihe.cn/bbs/post_share"),
			Params:  map[string]parse.ParamRule{"link_id": {}},
			Handle:  p.link,
		},
		{
			Keyword: "xiaoheihe.cn/app/bbs",
			Pattern: parse.Compile(`link/(?P<link_id>[A-Za-z0-9]+)`),
			Handle:  p.link,
		},
	}
}

type linkPart struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	URL     string `json:"url"`
	LiveURL string `json:"live_url"`
}

type heyboxUser struct {
	Avatar   string `json:"avatar"`
	Username string `json:"username"`
}

type heyboxLink struct {
	HasVideo     int64      `json:"has_video"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Text         string     `json:"text"`
	Click        int64      `json:"click"`
	CommentNum   int64      `json:"comment_num"`
	CreateAt     int64      `json:"create_at"`
	FavourCount  int64      `json:"favour_count"`
	LinkAwardNum int64      `json:"link_award_num"`
	ForwardNum   int64      `json:"forward_num"`
	User         heyboxUser `json:"user"`
	Battery      struct {
		Count *int64 `json:"count"`
	} `json:"battery"`
	VideoURL   string `json:"video_url"`
	VideoThumb string `json:"video_thumb"`
}

type treeResponse struct {
	Status string `json:"status"`
	Result struct {
		Link heyboxLink `json:"link"`
	} `json:"result"`
}

func (p *Parser) link(ctx context.Context, m *parse.Match) (*model.Result, error) {
	linkID := m.Get("link_id")
	token, err := p.ensureToken()
	if err != nil {
		return nil, err
	}

	raw, err := p.client.Get(linkURL(linkID), map[string]string{"Cookie": "x_xhh_tokenid=" + token})
	if err != nil {
		return nil, err
	}
	var got treeResponse
	if err := netx.Decode(raw, &got); err != nil {
		return nil, err
	}
	if got.Status != "ok" {
		return nil, errStatus(got.Status)
	}

	link := got.Result.Link
	stats := model.Stats{
		View:    htmlx.Num(link.Click),
		Like:    htmlx.Num(link.LinkAwardNum),
		Comment: htmlx.Num(link.CommentNum),
		Share:   htmlx.Num(link.ForwardNum),
		Collect: htmlx.Num(link.FavourCount),
	}
	if link.Battery.Count != nil {
		stats.Extra = [][2]string{{"电池", htmlx.Num(*link.Battery.Count)}}
	}

	return &model.Result{
		Platform: platform,
		URL:      "https://www.xiaoheihe.cn/app/bbs/link/" + linkID,
		Title:    link.Title,
		Author: model.Author{
			Name:   link.User.Username,
			Avatar: avatarURL(link.User.Avatar),
		},
		Stats:     stats,
		Timestamp: link.CreateAt,
		Content:   buildContent(link),
	}, nil
}

func buildContent(link heyboxLink) []model.Content {
	out := make([]model.Content, 0, 8)
	var parts []linkPart
	if err := json.Unmarshal([]byte(link.Text), &parts); err != nil {
		parts = nil
		if text := strings.TrimSpace(link.Text); text != "" {
			out = append(out, model.Text{Value: text})
		}
	}

	for _, part := range parts {
		switch part.Type {
		case "html":
			media := htmlx.Rich(part.Text)
			if media.Text != "" {
				out = append(out, model.Text{Value: media.Text})
			}
			for _, image := range media.Images {
				out = append(out, model.Image{URL: image})
			}
		case "text":
			if text := strings.TrimSpace(part.Text); text != "" {
				out = append(out, model.Text{Value: text})
			}
		case "img":
			if part.URL == "" {
				continue
			}
			if part.LiveURL != "" {
				out = append(out, model.LivePhoto{Image: part.URL, Stream: part.LiveURL, Headers: headers})
			} else {
				out = append(out, model.Image{URL: part.URL})
			}
		}
	}

	if link.HasVideo != 0 && link.VideoURL != "" && link.VideoThumb != "" {
		out = append(out, model.Video{URL: link.VideoURL, Cover: link.VideoThumb, Headers: headers})
	}
	if link.Description != "" && len(out) == 0 {
		out = append(out, model.Text{Value: link.Description})
	}
	return out
}

func avatarURL(raw string) string {
	if strings.Contains(raw, "?") && !strings.HasSuffix(raw, ">") && !strings.HasSuffix(raw, "%3E") {
		return raw + "\\"
	}
	return raw
}

func (p *Parser) ensureToken() (string, error) {
	if p.token != "" {
		return p.token, nil
	}
	payload, err := smPayload()
	if err != nil {
		return "", err
	}
	var res struct {
		Code   int `json:"code"`
		Detail struct {
			DeviceID string `json:"deviceId"`
		} `json:"detail"`
	}
	raw, err := p.client.PostJSON(tokenAPI, payload, nil)
	if err != nil {
		return "", err
	}
	if err := netx.Decode(raw, &res); err != nil {
		return "", err
	}
	if res.Code != 1100 || res.Detail.DeviceID == "" {
		return "", errorString("小黑盒 token 获取失败: " + truncate(string(raw)))
	}
	p.token = "B" + res.Detail.DeviceID
	return p.token, nil
}

func truncate(value string) string {
	if len(value) > 300 {
		return value[:300]
	}
	return value
}

const heyboxAlphabet = "AB45STUVWZEFGJ6CH01D237IXYPQRKLMN89"

func linkURL(linkID string) string {
	now := time.Now().Unix()
	return fmt.Sprintf("https://%s%s?os_type=web&app=heybox&client_type=web&version=999.0.4"+
		"&_time=%d&nonce=%s&hkey=%s&link_id=%s"+
		"&page=1&index=1&limit=5&x_client_type=weboutapp&x_app=heybox_website&x_os_type=Windows"+
		"&web_version=2.5", heyboxHost, heyboxPath, now, nonce(now), hkey(now), linkID)
}

func nonce(t int64) string {
	sum := md5.Sum([]byte(strconv.FormatInt(t, 10)))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func vm(e byte) byte {
	if e&0x80 != 0 {
		return (e << 1) ^ 27
	}
	return e << 1
}

func qm(e byte) byte { return vm(e) ^ e }

func mm(e byte) byte { return qm(vm(e)) }

func ym(e byte) byte { return mm(qm(vm(e))) }

func gm(e byte) byte { return ym(e) ^ mm(e) ^ qm(e) }

func mix(e []byte) []byte {
	return []byte{
		gm(e[0]) ^ ym(e[1]) ^ mm(e[2]) ^ qm(e[3]),
		qm(e[0]) ^ gm(e[1]) ^ ym(e[2]) ^ mm(e[3]),
		mm(e[0]) ^ qm(e[1]) ^ gm(e[2]) ^ ym(e[3]),
		ym(e[0]) ^ mm(e[1]) ^ qm(e[2]) ^ gm(e[3]),
	}
}

func pick(e, t string) string {
	if t == "" {
		return ""
	}
	out := make([]byte, 0, len(e))
	for i := 0; i < len(e); i++ {
		out = append(out, t[int(e[i])%len(t)])
	}
	return string(out)
}

func interleave(parts []string) string {
	longest := 0
	for _, part := range parts {
		if len(part) > longest {
			longest = len(part)
		}
	}
	var out strings.Builder
	for i := 0; i < longest; i++ {
		for _, part := range parts {
			if i < len(part) {
				out.WriteByte(part[i])
			}
		}
	}
	return out.String()
}

func prefix(value string, n int) string {
	if n < 0 {
		n += len(value)
	}
	if n < 0 || n > len(value) {
		return ""
	}
	return value[:n]
}

func hkey(t int64) string {
	segments := make([]string, 0, 4)
	for _, segment := range strings.Split(heyboxPath, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	path := "/" + strings.Join(segments, "/") + "/"

	hashed := md5hex(prefix(interleave([]string{
		pick(strconv.FormatInt(t+1, 10), prefix(heyboxAlphabet, -2)),
		pick(path, heyboxAlphabet),
		pick(nonce(t), heyboxAlphabet),
	}), 20))

	total := 0
	for _, value := range mix([]byte(hashed[len(hashed)-6:])) {
		total += int(value)
	}
	return pick(hashed[:5], prefix(heyboxAlphabet, -4)) + fmt.Sprintf("%02d", total%100)
}

type desRule struct {
	key        string
	obfuscated string
	encrypt    bool
}

var desRules = map[string]desRule{
	"appId":        {"uy7mzc4h", "xx", true},
	"box":          {"", "jf", false},
	"canvas":       {"snrn887t", "yk", true},
	"clientSize":   {"cpmjjgsu", "zx", true},
	"organization": {"78moqjfc", "dp", true},
	"os":           {"je6vk6t4", "pj", true},
	"platform":     {"pakxhcd2", "gm", true},
	"plugins":      {"v51m3pzl", "kq", true},
	"pmf":          {"2mdeslu3", "vw", true},
	"protocol":     {"", "protocol", false},
	"referer":      {"y7bmrjlc", "ab", true},
	"res":          {"whxqm2a7", "hf", true},
	"rtype":        {"x8o2h2bl", "lo", true},
	"sdkver":       {"9q3dcxp2", "sc", true},
	"status":       {"2jbrxxw4", "an", true},
	"subVersion":   {"eo3i2puh", "ns", true},
	"svm":          {"fzj3kaeh", "qr", true},
	"time":         {"q2t3odsk", "nb", true},
	"timezone":     {"1uv05lj5", "as", true},
	"tn":           {"x9nzj1bp", "py", true},
	"trees":        {"acfs0xo4", "pi", true},
	"ua":           {"k92crp1t", "bj", true},
	"url":          {"y95hjkoo", "cf", true},
	"version":      {"", "version", false},
	"vpw":          {"r9924ab5", "ca", true},
}

type smField struct {
	name   string
	text   string
	number int64
	isInt  bool
}

type tokenPayload struct {
	AppID        string `json:"appId"`
	Compress     int    `json:"compress"`
	Data         string `json:"data"`
	Encode       int    `json:"encode"`
	EP           string `json:"ep"`
	Organization string `json:"organization"`
	OS           string `json:"os"`
}

func smPayload() (tokenPayload, error) {
	uid := uuid4()
	priID := md5hex(uid)[:16]
	ep, err := rsaEncrypt(uid)
	if err != nil {
		return tokenPayload{}, err
	}

	now := time.Now()
	stamp := now.UnixMilli()
	fields := []smField{
		{name: "plugins", text: "MicrosoftEdgePDFPluginPortableDocumentFormatinternal-pdf-viewer1,MicrosoftEdgePDFViewermhjfbmdgcfjbbpaeojofohoefgiehjai1"},
		{name: "ua", text: heyboxUA},
		{name: "canvas", text: randomHex(8)},
		{name: "timezone", text: "-480", number: -480, isInt: true},
		{name: "platform", text: "Win32"},
		{name: "url", text: "https://www.xiaoheihe.cn/"},
		{name: "referer", text: ""},
		{name: "res", text: "1920_1080_24_1.25"},
		{name: "clientSize", text: "0_0_1080_1920_1920_1080_1920_1080"},
		{name: "status", text: "0011"},
		{name: "vpw", text: uuid4()},
		{name: "svm", text: strconv.FormatInt(stamp, 10), number: stamp, isInt: true},
		{name: "trees", text: uuid4()},
		{name: "pmf", text: strconv.FormatInt(stamp, 10), number: stamp, isInt: true},
		{name: "protocol", text: "102", number: 102, isInt: true},
		{name: "organization", text: smOrganize},
		{name: "appId", text: smAppID},
		{name: "os", text: "web"},
		{name: "version", text: "3.0.0"},
		{name: "sdkver", text: "3.0.0"},
		{name: "box", text: ""},
		{name: "rtype", text: "all"},
		{name: "smid", text: smid()},
		{name: "subVersion", text: "1.0.0"},
		{name: "time", text: "0", number: 0, isInt: true},
	}
	fields = append(fields, smField{name: "tn", text: md5hex(tnString(fields))})

	data, err := seal(pairs(fields), priID)
	if err != nil {
		return tokenPayload{}, err
	}

	return tokenPayload{
		AppID:        smAppID,
		Compress:     2,
		Data:         data,
		Encode:       5,
		EP:           ep,
		Organization: smOrganize,
		OS:           "web",
	}, nil
}

func tnString(fields []smField) string {
	sorted := make([]smField, len(fields))
	copy(sorted, fields)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })

	var out strings.Builder
	for _, field := range sorted {
		if field.isInt {
			out.WriteString(strconv.FormatInt(field.number*10000, 10))
			continue
		}
		out.WriteString(field.text)
	}
	return out.String()
}

func pairs(fields []smField) [][2]any {
	out := make([][2]any, 0, len(fields))
	for _, field := range fields {
		rule, ok := desRules[field.name]
		if !ok {
			out = append(out, [2]any{field.name, field.text})
			continue
		}
		var value any = field.text
		switch {
		case rule.encrypt:
			value = tripleDES(field.text, rule.key)
		case field.isInt:
			value = field.number
		}
		out = append(out, [2]any{rule.obfuscated, value})
	}
	return out
}

func tripleDES(value, key string) string {
	block, err := des.NewTripleDESCipher([]byte(key + key + key))
	if err != nil {
		return value
	}
	data := []byte(value)
	for len(data)%8 != 0 {
		data = append(data, 0)
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += 8 {
		block.Encrypt(out[i:i+8], data[i:i+8])
	}
	return base64.StdEncoding.EncodeToString(out)
}

func seal(fields [][2]any, priID string) (string, error) {
	var raw bytes.Buffer
	writer, err := gzip.NewWriterLevel(&raw, 2)
	if err != nil {
		return "", err
	}
	if _, err := writer.Write(compactJSON(fields)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	block, err := aes.NewCipher([]byte(priID))
	if err != nil {
		return "", err
	}
	data := append([]byte(base64.StdEncoding.EncodeToString(raw.Bytes())), 0)
	for len(data)%aes.BlockSize != 0 {
		data = append(data, 0)
	}
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, []byte("0102030405060708")).CryptBlocks(out, data)
	return hex.EncodeToString(out), nil
}

func compactJSON(fields [][2]any) []byte {
	var out strings.Builder
	out.WriteByte('{')
	for i, field := range fields {
		if i > 0 {
			out.WriteByte(',')
		}
		key, _ := json.Marshal(field[0])
		value, _ := json.Marshal(field[1])
		out.Write(key)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return []byte(out.String())
}

func rsaEncrypt(uid string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(smPublicKey)
	if err != nil {
		return "", err
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return "", err
	}
	public, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return "", errToken
	}
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, public, []byte(uid))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func smid() string {
	now := time.Now()
	stamp := fmt.Sprintf("%04d%02d%02d%02d%02d%02d", now.Year(), int(now.Month()), now.Day(),
		now.Hour(), now.Minute(), now.Second())
	value := stamp + md5hex(uuid4()) + "00"
	return value + md5hex("smsk_web_" + value)[:14] + "0"
}

func md5hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) string {
	buf := make([]byte, (n+1)/2)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)[:n]
}

func uuid4() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

type errorString string

func (e errorString) Error() string { return string(e) }

var errToken = errorString("小黑盒 token 获取失败")

func errStatus(status string) error {
	return errorString("小黑盒解析失败: " + status)
}
