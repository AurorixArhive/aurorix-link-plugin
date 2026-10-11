package qsmusic

import (
	"context"
	"fmt"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var iosHeaders = map[string]string{
	"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1 Edg/132.0.0.0",
}

var (
	shareLink  = parse.Compile(`https?://[^\s]*?qishui\.douyin\.com/s/[a-zA-Z0-9]+/`)
	routerData = parse.Compile(`(?s)<script\s+async=""\s+data-script-src="modern-inline">_ROUTER_DATA\s*=\s*(\{[\s\S]*?\});`)
)

var platform = model.Platform{Name: "qsmusic", DisplayName: "汽水音乐"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, iosHeaders)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{{
		Keyword: "qishui.douyin.com",
		Pattern: shareLink,
		Handle:  p.share,
	}}
}

type word struct {
	Text string `json:"text"`
}

type sentence struct {
	StartMs int64  `json:"startMs"`
	Text    string `json:"text"`
	Words   []word `json:"words"`
}

type lyrics struct {
	Sentences []sentence `json:"sentences"`
}

type albumInfo struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type trackInfo struct {
	Album albumInfo `json:"album"`
}

type audioWithLyricsOption struct {
	URL        string    `json:"url"`
	Duration   float64   `json:"duration"`
	ArtistName string    `json:"artistName"`
	TrackName  string    `json:"trackName"`
	CoverURL   string    `json:"coverURL"`
	TrackInfo  trackInfo `json:"trackInfo"`
	Lyrics     lyrics    `json:"lyrics"`
}

type trackPage struct {
	TrackID               string                `json:"track_id"`
	AudioWithLyricsOption audioWithLyricsOption `json:"audioWithLyricsOption"`
}

type routerDataStruct struct {
	LoaderData struct {
		TrackPage trackPage `json:"track_page"`
	} `json:"loaderData"`
}

func (a audioWithLyricsOption) toLRC() string {
	lines := make([]string, 0, len(a.Lyrics.Sentences))
	for _, item := range a.Lyrics.Sentences {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			var builder strings.Builder
			for _, w := range item.Words {
				builder.WriteString(w.Text)
			}
			text = strings.TrimSpace(builder.String())
		}
		if text == "" {
			continue
		}
		minutes := item.StartMs / 60000
		seconds := (item.StartMs % 60000) / 1000
		centiseconds := (item.StartMs % 1000) / 10
		lines = append(lines, fmt.Sprintf("[%02d:%02d.%02d]%s", minutes, seconds, centiseconds, text))
	}
	return strings.Join(lines, "\n")
}

func (p *Parser) share(ctx context.Context, m *parse.Match) (*model.Result, error) {
	page, err := p.client.Get(m.URL, iosHeaders)
	if err != nil {
		return nil, err
	}
	found := routerData.FindSubmatch(page)
	if found == nil {
		return nil, errNoData
	}

	var parsed routerDataStruct
	if err := netx.Decode(found[1], &parsed); err != nil {
		return nil, err
	}
	music := parsed.LoaderData.TrackPage.AudioWithLyricsOption

	audio := model.Audio{
		URL:      music.URL,
		Title:    music.TrackName,
		Artist:   music.ArtistName,
		Duration: music.Duration,
		Headers:  iosHeaders,
	}

	content := make([]model.Content, 0, 3)
	if music.CoverURL != "" {
		content = append(content, model.Image{URL: music.CoverURL})
	}
	content = append(content, audio)
	if text := music.toLRC(); text != "" {
		content = append(content, model.Text{Value: text})
	}

	return &model.Result{
		Platform: platform,
		URL:      m.URL,
		Title:    music.TrackName,
		Author:   model.Author{Name: music.ArtistName},
		Content:  content,
	}, nil
}

var errNoData = errorString("未找到结构化数据")

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
