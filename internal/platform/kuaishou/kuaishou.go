package kuaishou

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1",
	"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
}

var initState = regexp.MustCompile(`window\.INIT_STATE\s*=\s*(.*?)</script>`)

const photoInfoPath = "0sftu0xe0vhI6Bqq0qipup0tjnqmf"

var platform = model.Platform{Name: "kuaishou", DisplayName: "快手"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "v.kuaishou",
			Pattern: parse.Compile(`v\.kuaishou\.com/[A-Za-z\d._?%&+=\-/]+`),
			Handle:  p.photo,
		},
		{
			Keyword: "kuaishou.com",
			Pattern: parse.Compile(`(?:https?://)?(?:(?:www|v|m)\.)?kuaishou\.com/[A-Za-z\d._?%&+=\-/]+`),
			Handle:  p.photo,
		},
		{
			Keyword: "chenzhongtech",
			Pattern: parse.Compile(`(?:v\.m\.)?chenzhongtech\.com/fw/[A-Za-z\d._?%&+=\-/]+`),
			Handle:  p.photo,
		},
		{
			Keyword: "m.gifshow.com",
			Pattern: parse.Compile(`m\.gifshow\.com/fw/photo/[A-Za-z\d]+`),
			Handle:  p.photo,
		},
	}
}

type item struct {
	Caption      string `json:"caption"`
	Timestamp    int64  `json:"timestamp"`
	UserName     string `json:"userName"`
	HeadURL      string `json:"headUrl"`
	LikeCount    int64  `json:"likeCount"`
	CommentCount int64  `json:"commentCount"`
	ViewCount    int64  `json:"viewCount"`
	ShareCount   int64  `json:"shareCount"`
	Duration     int64  `json:"duration"`
	PhotoID      string `json:"photoId"`
	UserID       int64  `json:"userId"`
	MainMvUrls   []struct {
		URL string `json:"url"`
	} `json:"mainMvUrls"`
	CoverUrls []struct {
		URL string `json:"url"`
	} `json:"coverUrls"`
	ExtParams struct {
		Atlas *struct {
			CDNList []struct {
				CDN string `json:"cdn"`
			} `json:"cdnList"`
			List []string `json:"list"`
		} `json:"atlas"`
	} `json:"ext_params"`
}

func (p *Parser) photo(ctx context.Context, m *parse.Match) (*model.Result, error) {
	target := "https://" + strings.TrimPrefix(m.URL, "https://")
	if strings.HasPrefix(m.URL, "http://") {
		target = m.URL
	}
	final, err := p.client.FinalURL(target, nil)
	if err != nil {
		return nil, err
	}
	final = strings.Replace(final, "/fw/long-video/", "/fw/photo/", 1)

	page, err := p.client.Get(final, nil)
	if err != nil {
		return nil, err
	}
	found := initState.FindSubmatch(page)
	if found == nil {
		return nil, errNoState
	}

	var state map[string]json.RawMessage
	if err := json.Unmarshal(found[1], &state); err != nil {
		return nil, err
	}
	for key, value := range state {
		if !strings.Contains(key, photoInfoPath) {
			continue
		}
		var wrapper struct {
			Photo item `json:"photo"`
		}
		if err := json.Unmarshal(value, &wrapper); err != nil {
			return nil, err
		}
		return build(wrapper.Photo), nil
	}
	return nil, errNoState
}

func build(photo item) *model.Result {
	content := make([]model.Content, 0, 4)
	if text := htmlx.Compact(photo.Caption); text != "" {
		content = append(content, model.Text{Value: text})
	}
	if atlas := photo.ExtParams.Atlas; atlas != nil && len(atlas.CDNList) > 0 {
		cdn := atlas.CDNList[0].CDN
		for _, route := range atlas.List {
			content = append(content, model.Image{URL: "https://" + cdn + "/" + route})
		}
	} else if len(photo.MainMvUrls) > 0 {
		video := model.Video{
			URL:      photo.MainMvUrls[0].URL,
			Duration: float64(photo.Duration) / 1000,
			Headers:  headers,
		}
		if len(photo.CoverUrls) > 0 {
			video.Cover = photo.CoverUrls[0].URL
		}
		content = append(content, video)
	}

	return &model.Result{
		Platform:  platform,
		URL:       "https://m.gifshow.com/fw/photo/" + photo.PhotoID,
		Author:    model.Author{Name: strings.ReplaceAll(photo.UserName, "\u3164", ""), Avatar: photo.HeadURL},
		Timestamp: photo.Timestamp / 1000,
		Content:   content,
		Stats: model.Stats{
			View:    htmlx.Num(photo.ViewCount),
			Like:    htmlx.Num(photo.LikeCount),
			Share:   htmlx.Num(photo.ShareCount),
			Comment: htmlx.Num(photo.CommentCount),
		},
	}
}

var errNoState = errorString("快手页面里没有 INIT_STATE")

type errorString string

func (e errorString) Error() string { return string(e) }

var _ = http.MethodGet
