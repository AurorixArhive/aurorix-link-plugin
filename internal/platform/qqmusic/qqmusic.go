// Package qqmusic 解析 QQ 音乐的歌曲链接: 元信息走官方接口, 音频先试官方 vkey, 会员曲再问云汐。
package qqmusic

import (
	"context"
	"net/url"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

// 这三个地址测试里会指到假服务器上, 所以写成变量。
var (
	detailAPI  = "https://c.y.qq.com/v8/fcg-bin/fcg_play_single_song.fcg"
	vkeyAPI    = "https://u.y.qq.com/cgi-bin/musicu.fcg"
	resolveAPI = "https://a.aa.cab/qq.music"
)

const coverBase = "https://y.qq.com/music/photo_new/T002R800x800M000"

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://y.qq.com/",
}

var midPattern = parse.Compile(`(?:songDetail/|/song/|songmid["':=\s]+)([0-9A-Za-z]{10,})`)

var platform = model.Platform{Name: "qqmusic", DisplayName: "QQ音乐"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func init() { parse.Register(New) }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "y.qq.com",
			Pattern: parse.Compile(`y\.qq\.com/n/(?:ryqq|yqq)/(?:songDetail|song)/(?P<song_mid>[0-9A-Za-z]+)`),
			Handle:  p.song,
		},
		{
			Keyword: "c6.y.qq.com",
			Pattern: parse.Compile(`c6\.y\.qq\.com/base/fcgi-bin/u\?__=[0-9A-Za-z]+`),
			Handle:  p.short,
		},
	}
}

type singer struct {
	Name string `json:"name"`
}

type songInfo struct {
	Name     string   `json:"name"`
	Interval int64    `json:"interval"`
	Singer   []singer `json:"singer"`
	Album    struct {
		Mid string `json:"mid"`
	} `json:"album"`
	File struct {
		MediaMid string `json:"media_mid"`
	} `json:"file"`
}

type detailResp struct {
	Data []songInfo `json:"data"`
}

type vkeyResp struct {
	Req0 struct {
		Data struct {
			Midurlinfo []struct {
				Purl string `json:"purl"`
			} `json:"midurlinfo"`
		} `json:"data"`
	} `json:"req_0"`
}

type resolveResp struct {
	Data struct {
		Song   string `json:"song"`
		Singer string `json:"singer"`
		Music  string `json:"music"`
	} `json:"data"`
}

func (p *Parser) song(ctx context.Context, m *parse.Match) (*model.Result, error) {
	mid := m.Get("song_mid")
	if mid == "" {
		return nil, errNoMid
	}
	return p.byMid(ctx, mid, "")
}

// short 短链跟到底, 再从落地的地址或页面里抠 songmid。
func (p *Parser) short(ctx context.Context, m *parse.Match) (*model.Result, error) {
	page, final, err := p.client.GetFinal(m.URL, nil)
	if err != nil {
		return nil, err
	}
	if mid := songMidIn(string(page)); mid != "" {
		return p.byMid(ctx, mid, final)
	}
	if mid := songMidIn(final); mid != "" {
		return p.byMid(ctx, mid, final)
	}
	return nil, errNoMid
}

func (p *Parser) byMid(ctx context.Context, mid, pageURL string) (*model.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := url.Values{
		"songmid": {mid}, "platform": {"yqq.json"}, "format": {"json"},
		"inCharset": {"utf8"}, "outCharset": {"utf-8"}, "needNewCode": {"1"},
	}
	var resp detailResp
	if err := p.client.GetJSON(detailAPI+"?"+query.Encode(), &resp, nil); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || strings.TrimSpace(resp.Data[0].Name) == "" {
		return nil, errNoSong
	}
	info := resp.Data[0]
	title, artist := strings.TrimSpace(info.Name), singers(info.Singer)

	content := make([]model.Content, 0, 2)
	if cover := coverBase + strings.TrimSpace(info.Album.Mid) + ".jpg"; len(info.Album.Mid) > 0 {
		content = append(content, model.Image{URL: cover})
	}
	if link := p.audioLink(ctx, mid, info, title, artist); link != "" {
		content = append(content, model.Audio{
			URL: link, Title: title, Artist: artist, Duration: float64(info.Interval),
		})
	}

	return &model.Result{
		Platform: platform,
		URL:      nonEmpty(withScheme(pageURL), "https://y.qq.com/n/ryqq/songDetail/"+mid),
		Title:    title,
		Author:   model.Author{Name: artist},
		Content:  content,
	}, nil
}

// audioLink 先问官方 vkey: 免费曲它直接给链, 会员曲匿名只回空 purl, 那时再按歌名歌手问云汐。
func (p *Parser) audioLink(ctx context.Context, mid string, info songInfo, title, artist string) string {
	if link := p.vkeyLink(mid, info.File.MediaMid); link != "" {
		return link
	}
	return p.yunxiLink(ctx, title, artist)
}

func (p *Parser) vkeyLink(mid, mediaMid string) string {
	if mid == "" || mediaMid == "" {
		return ""
	}
	payload := map[string]any{
		"comm": map[string]any{"uin": "0", "format": "json", "ct": 24, "cv": 0},
		"req_0": map[string]any{
			"module": "vkey.GetVkeyServer",
			"method": "CgiGetVkey",
			"param": map[string]any{
				"guid":     "10000",
				"songmid":  []string{mid},
				"songtype": []int{0},
				"uin":      "0",
				"ctx":      1,
				"filename": []string{"M800" + mediaMid + ".mp3", "M500" + mediaMid + ".mp3"},
			},
		},
	}
	var resp vkeyResp
	if err := p.client.PostJSONDecode(vkeyAPI, payload, &resp, nil); err != nil {
		return ""
	}
	for _, item := range resp.Req0.Data.Midurlinfo {
		if purl := strings.TrimSpace(item.Purl); purl != "" {
			return "https://ws.stream.qqmusic.qq.com/" + purl
		}
	}
	return ""
}

// yunxiLink 按歌名+歌手要链: 会员曲只有这条路拿得到。对不上号就放弃 —— 按名字查很容易撞翻唱。
func (p *Parser) yunxiLink(ctx context.Context, title, artist string) string {
	if err := ctx.Err(); err != nil {
		return ""
	}
	keyword := title
	if first := firstArtist(artist); first != "" {
		keyword = title + " " + first
	}
	query := url.Values{"msg": {keyword}, "n": {"1"}, "type": {"1"}}
	var resp resolveResp
	if err := p.client.GetJSON(resolveAPI+"?"+query.Encode(), &resp, nil); err != nil {
		return ""
	}
	if !strings.EqualFold(title, strings.TrimSpace(resp.Data.Song)) {
		return ""
	}
	if first := firstArtist(artist); first != "" && !strings.Contains(resp.Data.Singer, first) {
		return ""
	}
	return strings.TrimSpace(resp.Data.Music)
}

func songMidIn(text string) string {
	found := midPattern.FindStringSubmatch(text)
	if found == nil {
		return ""
	}
	return found[1]
}

func singers(list []singer) string {
	names := make([]string, 0, len(list))
	for _, item := range list {
		if name := strings.TrimSpace(item.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, "、")
}

// firstArtist 歌手串有 "A、B" 也有 "A,B", 取第一个就够了。
func firstArtist(artist string) string {
	parts := strings.FieldsFunc(artist, func(r rune) bool {
		return r == '、' || r == ',' || r == '，' || r == '/' || r == '&'
	})
	if len(parts) == 0 {
		return strings.TrimSpace(artist)
	}
	return strings.TrimSpace(parts[0])
}

func withScheme(rawURL string) string {
	if rawURL == "" || strings.HasPrefix(rawURL, "http") {
		return rawURL
	}
	return "https://" + rawURL
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var (
	errNoMid  = errorString("QQ音乐没认出歌曲 ID")
	errNoSong = errorString("QQ音乐取不到这首歌的信息(可能已下架)")
)

type errorString string

func (e errorString) Error() string { return string(e) }
