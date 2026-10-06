package parse

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"video-plugin/internal/model"
)

const (
	heyboxTreePath   = "/bbs/app/link/tree/v2"
	heyboxSharePath  = "/v3/bbs/app/api/web/share"
	heyboxAlphabet   = "AB45STUVWZEFGJ6CH01D237IXYPQRKLMN89"
	heyboxTreeAPI    = "https://api.xiaoheihe.cn" + heyboxTreePath
	heyboxShareAPI   = "https://api.xiaoheihe.cn" + heyboxSharePath
	heyboxUserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15"
	heyboxImageLimit = 9
)

var (
	heyboxLinkIDPattern = regexp.MustCompile(`(?:link_id=|/link/)([0-9a-fA-F]{6,})`)
	imgSrcPattern       = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["']`)
	htmlTagPattern      = regexp.MustCompile(`<[^>]*>`)
)

type heybox struct {
	env *env
}

func newHeybox(env *env) *heybox { return &heybox{env: env} }

func (h *heybox) Parse(rawURL string) (model.Media, error) {
	linkID := heyboxLinkID(rawURL)
	if linkID == "" {
		// 分享短链要把跳转后的地址拿来看, link_id 在那一跳里
		resp, err := h.env.Get(rawURL, h.headers(""))
		if err != nil {
			return model.Media{}, fmt.Errorf("小黑盒分享页打不开: %w", err)
		}
		linkID = nonEmpty(heyboxLinkID(resp.URL), heyboxLinkID(resp.Text()))
	}
	if linkID == "" {
		return model.Media{}, errors.New("没从这个链接里认出小黑盒作品号")
	}

	link, err := h.tree(linkID)
	if err != nil {
		return model.Media{}, err
	}
	return heyboxMedia(link, "https://www.xiaoheihe.cn/app/bbs/link/"+linkID), nil
}

func (h *heybox) headers(cookie string) map[string]string {
	head := map[string]string{
		"User-Agent": heyboxUserAgent,
		"Referer":    "https://www.xiaoheihe.cn/",
	}
	if cookie != "" {
		head["Cookie"] = cookie
	}
	return head
}

// tree 取作品正文。小黑盒的 web 接口要带 hkey 签名, 不带一律回风控页。
func (h *heybox) tree(linkID string) (map[string]any, error) {
	hkey, ts, nonce := heyboxSign(heyboxTreePath)
	query := url.Values{
		"link_id":       {linkID},
		"hkey":          {hkey},
		"_time":         {strconv.FormatInt(ts, 10)},
		"nonce":         {nonce},
		"x_client_type": {"web"},
		"os_type":       {"web"},
		"x_os_type":     {"Mac"},
		"client_type":   {"web"},
		"x_app":         {"heybox_website"},
		"web_version":   {"2.5"},
		"heybox_id":     {"-1"},
		"version":       {"999.0.4"},
		"app":           {"heybox"},
	}
	resp, err := h.env.Get(heyboxTreeAPI+"?"+query.Encode(), h.headers(""))
	if err != nil {
		return nil, fmt.Errorf("小黑盒接口请求失败: %w", err)
	}
	var body struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
		Result struct {
			Link map[string]any `json:"link"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil, errors.New("小黑盒返回的数据看不懂")
	}
	if body.Status != "ok" || body.Result.Link == nil {
		return nil, fmt.Errorf("小黑盒没给正文: %s", nonEmpty(body.Msg, body.Status, "可能需要验证或登录"))
	}
	return body.Result.Link, nil
}

func heyboxLinkID(rawURL string) string {
	if m := heyboxLinkIDPattern.FindStringSubmatch(rawURL); m != nil {
		return m[1]
	}
	return ""
}

func heyboxMedia(link map[string]any, pageURL string) model.Media {
	body := str(link["body"])
	media := model.Media{
		Platform: "小黑盒",
		Title:    firstLine(nonEmpty(str(link["title"]), plainText(body))),
		Author:   firstString(dict(link["user"]), "username", "nickname"),
		Desc:     firstLine(plainText(nonEmpty(str(link["description"]), body))),
		Cover:    firstHTTP(link["thumb"], link["cover"]),
		PageURL:  pageURL,
	}
	if media.Title == "" {
		media.Title = "小黑盒帖子"
	}
	if media.Cover == "" {
		if images := htmlImages(body); len(images) > 0 {
			media.Cover = images[0]
		}
	}
	if url := firstHTTP(link["video_url"], link["videoUrl"], link["video"]); url != "" {
		media.Video = &model.Video{
			URL:     url,
			Headers: map[string]string{"User-Agent": heyboxUserAgent, "Referer": "https://www.xiaoheihe.cn/"},
		}
		return media
	}
	media.Images = htmlImages(body)
	return media
}

// htmlImages 从正文 HTML 里按顺序取图片地址。
func htmlImages(body string) []string {
	matches := imgSrcPattern.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		src := strings.TrimSpace(m[1])
		if !strings.HasPrefix(src, "http") || seen[src] {
			continue
		}
		seen[src] = true
		out = append(out, src)
		if len(out) >= heyboxImageLimit {
			break
		}
	}
	return out
}

func plainText(body string) string {
	// 标签换成空格后常留下连续空白, 折叠一下, 否则文案里会夹一串空格
	return strings.Join(strings.Fields(htmlTagPattern.ReplaceAllString(body, " ")), " ")
}

// --- 小黑盒 web 端接口签名 ---
//
// 服务端按这套变换校验 hkey, 少一步就回风控页, 所以这里必须逐字节一致:
// 取路径、时间戳与 nonce 三列, 用同一张字符表各自映射后交织, 取前 20 字符做 MD5,
// 再用 MD5 尾 6 字节做一轮字节混合算出两位校验和。

func heyboxSign(pathname string) (hkey string, ts int64, nonce string) {
	ts = time.Now().Unix()
	sum := md5.Sum([]byte(strconv.FormatInt(ts, 10) + strconv.FormatUint(rand.Uint64(), 10)))
	nonce = strings.ToUpper(hex.EncodeToString(sum[:]))
	return heyboxSignAt(pathname, ts, nonce), ts, nonce
}

// heyboxSignAt 是签名里与当前时间无关的那部分, 单独拆出来便于按固定时间核对结果。
func heyboxSignAt(pathname string, ts int64, nonce string) string {
	path := "/" + strings.Join(nonEmptyParts(strings.Split(pathname, "/")), "/") + "/"
	cols := [][]byte{
		heyboxMap(strconv.FormatInt(ts+1, 10), -2),
		heyboxSubstitute(path),
		heyboxSubstitute(nonce),
	}

	interleaved := make([]byte, 0, 20)
	for i := 0; ; i++ {
		appended := false
		for _, col := range cols {
			if i < len(col) {
				interleaved = append(interleaved, col[i])
				appended = true
			}
		}
		if !appended || len(interleaved) >= 20 {
			break
		}
	}
	if len(interleaved) > 20 {
		interleaved = interleaved[:20]
	}

	digest := md5.Sum(interleaved)
	hexed := hex.EncodeToString(digest[:])
	check := heyboxChecksum([]byte(hexed[len(hexed)-6:]))
	return string(heyboxMap(hexed[:5], -4)) + check
}

func nonEmptyParts(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// heyboxSubstitute 用字符表逐字节替换。
func heyboxSubstitute(text string) []byte {
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		out = append(out, heyboxAlphabet[int(text[i])%len(heyboxAlphabet)])
	}
	return out
}

// heyboxMap 取字符表前缀(keep 为负表示去掉尾部若干字符)再逐字节替换。
func heyboxMap(text string, keep int) []byte {
	table := heyboxAlphabet
	if keep < 0 {
		table = heyboxAlphabet[:len(heyboxAlphabet)+keep]
	}
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		out = append(out, table[int(text[i])%len(table)])
	}
	return out
}

func heyboxChecksum(last6 []byte) string {
	mixed := heyboxMix(last6)
	total := 0
	for _, b := range mixed {
		total += int(b)
	}
	return fmt.Sprintf("%02d", total%100)
}

// heyboxMix 一轮字节混合, 与 GF(2^8) 上的乘 2 及其组合有关。
func heyboxMix(v []byte) []byte {
	out := make([]byte, 4)
	out[0] = heyboxG(v[0]) ^ heyboxY(v[1]) ^ heyboxQ2(v[2]) ^ heyboxQ(v[3])
	out[1] = heyboxQ(v[0]) ^ heyboxG(v[1]) ^ heyboxY(v[2]) ^ heyboxQ2(v[3])
	out[2] = heyboxQ2(v[0]) ^ heyboxQ(v[1]) ^ heyboxG(v[2]) ^ heyboxY(v[3])
	out[3] = heyboxY(v[0]) ^ heyboxQ2(v[1]) ^ heyboxQ(v[2]) ^ heyboxG(v[3])
	return out
}

func heyboxTimes2(b byte) byte {
	if b&128 != 0 {
		return (b << 1) ^ 27
	}
	return b << 1
}

func heyboxQ(b byte) byte  { return heyboxTimes2(b) ^ b }
func heyboxQ2(b byte) byte { return heyboxQ(heyboxTimes2(b)) }
func heyboxY(b byte) byte  { return heyboxQ2(heyboxQ(heyboxTimes2(b))) }
func heyboxG(b byte) byte  { return heyboxY(b) ^ heyboxQ2(b) ^ heyboxQ(b) }
