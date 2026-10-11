package kugou

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
}

var (
	shortLink = parse.Compile(`https?://t1\.kugou\.com/[a-zA-Z0-9]+`)
	hashSite  = parse.Domain("t1.kugou.com")
	sharePage = parse.Compile(`https?://[^\s]*?kugou\.com.*?(?:/(?:share|mixsong)/[a-zA-Z0-9]+\.html|(?:id|chain)=[a-zA-Z0-9]+)`)
	smartyVar = parse.Compile(`(?s)var dataFromSmarty\s*=\s*(\[.*?\]),`)
)

var platform = model.Platform{Name: "kugou", DisplayName: "酷狗音乐"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "t1.kugou.com",
			Pattern: hashSite,
			Params:  map[string]parse.ParamRule{"hash": {}},
			Handle:  p.byHashMatch,
		},
		{
			Keyword: "t1.kugou.com",
			Pattern: shortLink,
			Handle:  p.short,
		},
		{
			Keyword: "kugou.com",
			Handle:  p.share,
		},
	}
}

type playInfo struct {
	ErrCode      int64  `json:"errcode"`
	Error        string `json:"error"`
	URL          string `json:"url"`
	Hash         string `json:"hash"`
	SingerName   string `json:"singerName"`
	SongName     string `json:"songName"`
	AlbumImg     string `json:"album_img"`
	TimeLength   int64  `json:"timeLength"`
	AlbumAudioID int64  `json:"album_audio_id"`
}

type krcsSearch struct {
	ErrCode    int64  `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
	Candidates []struct {
		ID        string `json:"id"`
		AccessKey string `json:"accesskey"`
	} `json:"candidates"`
}

type lyric struct {
	ErrorCode int64  `json:"error_code"`
	Info      string `json:"info"`
	Charset   string `json:"charset"`
	Content   string `json:"content"`
}

func (p *Parser) short(ctx context.Context, m *parse.Match) (*model.Result, error) {
	final, err := p.client.FinalURL(m.URL, nil)
	if err != nil {
		return nil, err
	}
	return p.dispatch(ctx, final)
}

func (p *Parser) dispatch(ctx context.Context, rawURL string) (*model.Result, error) {
	routes := p.Routes()
	for _, route := range routes[1:] {
		if !strings.Contains(rawURL, route.Keyword) {
			continue
		}
		groups := route.Pattern.FindStringSubmatch(rawURL)
		if groups == nil {
			continue
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
		}
		return route.Handle(ctx, &parse.Match{URL: groups[0], Params: params})
	}
	return nil, parse.ErrNoMatch
}

func (p *Parser) byHashMatch(ctx context.Context, m *parse.Match) (*model.Result, error) {
	return p.byHash(ctx, m.Get("hash"))
}

func (p *Parser) share(ctx context.Context, m *parse.Match) (*model.Result, error) {
	page, err := p.client.Get(m.URL, nil)
	if err != nil {
		return nil, err
	}
	hash := extractHash(page)
	if hash == "" {
		return nil, errNoHash
	}
	return p.byHash(ctx, hash)
}

func extractHash(page []byte) string {
	found := smartyVar.FindSubmatch(page)
	if found == nil {
		return ""
	}
	var items []struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(found[1], &items); err != nil {
		return ""
	}
	if len(items) == 0 {
		return ""
	}
	return strings.ToUpper(items[0].Hash)
}

func (p *Parser) byHash(ctx context.Context, hash string) (*model.Result, error) {
	var info playInfo
	if err := p.client.GetJSON("https://m.kugou.com/app/i/getSongInfo.php?cmd=playInfo&hash="+hash, &info, nil); err != nil {
		return nil, err
	}
	if info.ErrCode != 0 {
		return nil, fmt.Errorf("酷狗音乐解析失败: %d %s", info.ErrCode, info.Error)
	}
	if info.URL == "" {
		return nil, errNoAudio
	}

	audio := model.Audio{
		URL:      info.URL,
		Title:    info.SongName,
		Artist:   info.SingerName,
		Duration: float64(info.TimeLength),
	}

	var krcs krcsSearch
	if err := p.client.GetJSON("https://krcs.kugou.com/search?hash="+info.Hash, &krcs, nil); err != nil {
		return nil, err
	}
	if krcs.ErrCode != 200 {
		return nil, fmt.Errorf("酷狗音乐解析失败: 歌词搜索失败: %s", krcs.ErrMsg)
	}
	if len(krcs.Candidates) == 0 {
		return nil, errNoLyric
	}

	candidate := krcs.Candidates[0]
	var got lyric
	link := "https://lyrics.kugou.com/download?ver=1&id=" + candidate.ID + "&accesskey=" + candidate.AccessKey + "&fmt=lrc"
	if err := p.client.GetJSON(link, &got, nil); err != nil {
		return nil, err
	}
	if got.ErrorCode != 0 {
		return nil, fmt.Errorf("酷狗音乐解析失败: 歌词获取失败: %s", got.Info)
	}
	text := ""
	if decoded, err := base64.StdEncoding.DecodeString(got.Content); err == nil {
		text = string(decoded)
	}
	cover := strings.Replace(info.AlbumImg, "{size}", "480", 1)

	content := make([]model.Content, 0, 3)
	if cover != "" {
		content = append(content, model.Image{URL: cover})
	}
	content = append(content, audio)
	if text != "" {
		content = append(content, model.Text{Value: text})
	}

	return &model.Result{
		Platform: platform,
		URL:      fmt.Sprintf("https://h5.kugou.com/v2/v-5a15aeb1/index.html?album_audio_id=%d", info.AlbumAudioID),
		Title:    info.SongName,
		Author:   model.Author{Name: info.SingerName},
		Content:  content,
	}, nil
}

var (
	errNoHash  = errorString("未找到歌曲hash")
	errNoAudio = errorString("未找到音频资源")
	errNoLyric = errorString("未找到歌词")
)

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
