package kuwo

import (
	"context"
	"fmt"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
}

var playDetail = parse.Compile(`play_detail/(?P<id>\d+)`)

var platform = model.Platform{Name: "kuwo", DisplayName: "酷我音乐"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{{
		Keyword: "kuwo.cn",
		Pattern: playDetail,
		Handle:  p.song,
	}}
}

type envelope struct {
	Code int64 `json:"code"`
	Data struct {
		Title           string  `json:"title"`
		Artist          string  `json:"artist"`
		ArtistPic       string  `json:"artist_pic"`
		Album           string  `json:"album"`
		Cover           string  `json:"cover"`
		Lyric           string  `json:"lyric"`
		DownloadURL     string  `json:"download_url"`
		DurationSeconds float64 `json:"duration_seconds"`
		Quality         struct {
			Code string `json:"code"`
			Name string `json:"name"`
		} `json:"quality"`
	} `json:"data"`
}

func (p *Parser) song(ctx context.Context, m *parse.Match) (*model.Result, error) {
	rid := m.Get("id")

	var got envelope
	endpoint := "https://parse-api.sokoko.org/api/kuwo/song/?music_id=" + rid + "&quality=320k"
	if err := p.client.GetJSON(endpoint, &got, nil); err != nil {
		return nil, err
	}
	if got.Code != 200 {
		return nil, fmt.Errorf("酷我音乐接口返回错误: code=%d", got.Code)
	}
	data := got.Data
	if !strings.HasPrefix(data.DownloadURL, "http") {
		return nil, errBadURL
	}

	audio := model.Audio{
		URL:      data.DownloadURL,
		Title:    data.Title,
		Artist:   data.Artist,
		Duration: data.DurationSeconds,
	}

	content := make([]model.Content, 0, 3)
	if data.Cover != "" {
		content = append(content, model.Image{URL: data.Cover})
	}
	content = append(content, audio)
	if data.Lyric != "" {
		content = append(content, model.Text{Value: data.Lyric})
	}

	return &model.Result{
		Platform: platform,
		URL:      "https://www.kuwo.cn/play_detail/" + rid,
		Title:    data.Title,
		Author:   model.Author{Name: data.Artist, Avatar: data.ArtistPic},
		Content:  content,
	}, nil
}

var errBadURL = errorString("无效音乐URL")

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
