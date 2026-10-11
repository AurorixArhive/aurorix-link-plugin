package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"Referer": "https://wyapi.toubiec.cn/",
}

const apiBase = "https://nextmusic.toubiec.cn/api"

var (
	songIDPath = parse.Compile(`song/(?P<id>\d+)`)
	shortLink  = parse.Compile(`https?://[^\s]*?163cn\.tv/[a-zA-Z0-9]+`)
	musicSite  = parse.Domain("music.163.com")
)

var platform = model.Platform{Name: "netease", DisplayName: "网易云音乐"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "music.163.com",
			Pattern: songIDPath,
			Handle:  p.song,
		},
		{
			Keyword: "music.163.com",
			Pattern: musicSite,
			Params:  map[string]parse.ParamRule{"id": {AsInt: true}},
			Handle:  p.song,
		},
		{
			Keyword: "163cn.tv",
			Pattern: shortLink,
			Handle:  p.short,
		},
	}
}

type envelope struct {
	Code int64           `json:"code"`
	Data json.RawMessage `json:"data"`
}

type songInfo struct {
	Name     string `json:"name"`
	Singer   string `json:"singer"`
	PicImg   string `json:"picimg"`
	Duration string `json:"duration"`
}

type songURL struct {
	URL string `json:"url"`
}

type songLyric struct {
	LRC string `json:"lrc"`
}

func randomIP() string {
	return fmt.Sprintf("%d.%d.%d.%d", rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

func (p *Parser) fetch(endpoint string, payload map[string]any, out any) error {
	payload["timestamp"] = time.Now().UnixMilli()
	payload["ip"] = randomIP()
	raw, err := p.client.PostJSON(apiBase+"/"+endpoint, payload, nil)
	if err != nil {
		return err
	}
	var env envelope
	if err := netx.Decode(raw, &env); err != nil {
		return err
	}
	if env.Code != 200 {
		return fmt.Errorf("网易云接口返回错误: code=%d", env.Code)
	}
	return netx.Decode(env.Data, out)
}

func (p *Parser) short(ctx context.Context, m *parse.Match) (*model.Result, error) {
	final, err := p.client.FinalURL(m.URL, nil)
	if err != nil {
		return nil, err
	}
	return p.dispatch(ctx, final)
}

func (p *Parser) dispatch(ctx context.Context, rawURL string) (*model.Result, error) {
	routes := p.Routes()[:2]
	for _, route := range routes {
		if !strings.Contains(rawURL, route.Keyword) {
			continue
		}
		groups := route.Pattern.FindStringSubmatch(rawURL)
		if groups == nil {
			continue
		}
		params := namedGroups(route.Pattern, groups)
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

func namedGroups(re *regexp.Regexp, groups []string) map[string]string {
	params := map[string]string{}
	for i, name := range re.SubexpNames() {
		if i > 0 && i < len(groups) && name != "" && groups[i] != "" {
			params[name] = groups[i]
		}
	}
	return params
}

func (p *Parser) song(ctx context.Context, m *parse.Match) (*model.Result, error) {
	id := m.Get("id")

	var info songInfo
	if err := p.fetch("getSongInfo", map[string]any{"id": id}, &info); err != nil {
		return nil, err
	}
	title := info.Name
	if title == "" {
		title = "未知"
	}
	artist := info.Singer
	if artist == "" {
		artist = "未知歌手"
	}
	duration := parseDuration(info.Duration)

	var lyricData songLyric
	lyric := ""
	if err := p.fetch("getSongLyric", map[string]any{"id": id}, &lyricData); err == nil {
		lyric = lyricData.LRC
	}

	audioURL := ""
	for _, level := range []string{"lossless", "standard"} {
		var data songURL
		if err := p.fetch("getSongUrl", map[string]any{"id": id, "level": level}, &data); err != nil {
			continue
		}
		if data.URL != "" {
			audioURL = data.URL
			break
		}
	}
	if audioURL == "" {
		return nil, errNoAudio
	}

	content := make([]model.Content, 0, 3)
	content = append(content, model.Audio{URL: audioURL, Title: title, Artist: artist, Duration: duration})
	if info.PicImg != "" {
		content = append(content, model.Image{URL: info.PicImg})
	}
	if lyric != "" {
		content = append(content, model.Text{Value: lyric})
	}

	return &model.Result{
		Platform: platform,
		URL:      "https://music.163.com/song/" + id,
		Title:    title,
		Author:   model.Author{Name: artist},
		Content:  content,
	}, nil
}

func parseDuration(value string) float64 {
	parts := strings.Split(value, ":")
	if len(parts) < 1 || len(parts) > 3 {
		return 0
	}
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return 0
		}
		numbers = append(numbers, number)
	}
	var hours, minutes, seconds int
	switch len(numbers) {
	case 1:
		seconds = numbers[0]
	case 2:
		minutes, seconds = numbers[0], numbers[1]
	default:
		hours, minutes, seconds = numbers[0], numbers[1], numbers[2]
	}
	if seconds < 0 || seconds >= 60 || minutes < 0 || minutes >= 60 || hours < 0 {
		return 0
	}
	return float64(hours*3600 + minutes*60 + seconds)
}

var errNoAudio = errorString("无法获取音频下载地址")

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
