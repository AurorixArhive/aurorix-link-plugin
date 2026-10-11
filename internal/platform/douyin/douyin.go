package douyin

import (
	"context"
	"net/url"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://www.douyin.com/",
}

const (
	detailAPI = "https://www.douyin.com/aweme/v1/web/aweme/detail/"
	playAPI   = "https://aweme.snssdk.com/aweme/v1/play/"
)

var platform = model.Platform{Name: "douyin", DisplayName: "抖音"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "v.douyin",
			Pattern: parse.Compile(`v\.douyin\.com/[a-zA-Z0-9_\-]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "jx.douyin",
			Pattern: parse.Compile(`jx\.douyin\.com/[a-zA-Z0-9_\-]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "douyin.com",
			Pattern: parse.Compile(`douyin\.com/[a-z]+/(?P<aweme_id>\d+)`),
			Handle:  p.work,
		},
		{
			Keyword: "iesdouyin.com",
			Pattern: parse.Compile(`iesdouyin\.com/share/[a-z]+/(?P<aweme_id>\d+)`),
			Handle:  p.work,
		},
		{
			Keyword: "m.douyin.com",
			Pattern: parse.Compile(`m\.douyin\.com/share/[a-z]+/(?P<aweme_id>\d+)`),
			Handle:  p.work,
		},
		{
			Keyword: "jingxuan.douyin.com",
			Pattern: parse.Compile(`jingxuan\.douyin\.com/m/[a-z]+/(?P<aweme_id>\d+)`),
			Handle:  p.work,
		},
	}
}

type addr struct {
	URI     string   `json:"uri"`
	URLList []string `json:"url_list"`
}

func (a addr) url() string {
	base := playAPI + "?video_id=" + a.URI
	if len(a.URLList) == 0 {
		return base
	}
	parsed, err := url.Parse(a.URLList[len(a.URLList)-1])
	if err != nil {
		return base
	}
	if fileID := parsed.Query().Get("file_id"); fileID != "" {
		return base + "&file_id=" + fileID
	}
	return base
}

type urlList struct {
	URLList []string `json:"url_list"`
}

type statistics struct {
	CommentCount int64 `json:"comment_count"`
	DiggCount    int64 `json:"digg_count"`
	ShareCount   int64 `json:"share_count"`
	CollectCount int64 `json:"collect_count"`
}

type author struct {
	UID         string  `json:"uid"`
	Nickname    string  `json:"nickname"`
	AvatarThumb urlList `json:"avatar_thumb"`
	Signature   string  `json:"signature"`
}

type video struct {
	Cover              urlList  `json:"cover"`
	Duration           int64    `json:"duration"`
	PlayAddr           addr     `json:"play_addr"`
	CoverOriginalScale *urlList `json:"cover_original_scale"`
}

type image struct {
	URLList  []string `json:"url_list"`
	URI      string   `json:"uri"`
	ClipType *int64   `json:"clip_type"`
	Video    *video   `json:"video"`
}

type musicPlayURL struct {
	URI string `json:"uri"`
}

type music struct {
	Duration        int64        `json:"duration"`
	MID             string       `json:"mid"`
	PlayURL         musicPlayURL `json:"play_url"`
	Extra           string       `json:"extra"`
	IsOriginalSound bool         `json:"is_original_sound"`
}

type shareInfo struct {
	ShareDesc     string `json:"share_desc"`
	ShareDescInfo string `json:"share_desc_info"`
}

type aweme struct {
	AwemeID    string     `json:"aweme_id"`
	Author     author     `json:"author"`
	ShareInfo  shareInfo  `json:"share_info"`
	Statistics statistics `json:"statistics"`
	CreateTime int64      `json:"create_time"`
	ShareURL   string     `json:"share_url"`
	Region     string     `json:"region"`
	IsSlides   bool       `json:"is_slides"`
	Images     []image    `json:"images"`
	Music      *music     `json:"music"`
	Video      *video     `json:"video"`
}

type response struct {
	AwemeDetail aweme `json:"aweme_detail"`
}

func (p *Parser) shortLink(ctx context.Context, m *parse.Match) (*model.Result, error) {
	target := m.URL
	if !strings.HasPrefix(target, "http") {
		target = "https://" + target
	}
	final, err := p.client.FinalURL(target, nil)
	if err != nil {
		return nil, err
	}
	for _, route := range p.Routes() {
		groups := route.Pattern.FindStringSubmatch(final)
		if groups == nil {
			continue
		}
		params := map[string]string{}
		for i, name := range route.Pattern.SubexpNames() {
			if i > 0 && i < len(groups) && name != "" && groups[i] != "" {
				params[name] = groups[i]
			}
		}
		return route.Handle(ctx, &parse.Match{URL: groups[0], Params: params})
	}
	return nil, errNoRoute
}

func (p *Parser) work(ctx context.Context, m *parse.Match) (*model.Result, error) {
	awemeID := m.Get("aweme_id")
	raw, err := p.client.Get(detailAPI+"?aweme_id="+awemeID+"&aid=6383", map[string]string{
		"Origin":  "https://open.douyin.com",
		"Referer": "https://open.douyin.com/",
	})
	if err != nil {
		return nil, err
	}
	var got response
	if err := netx.Decode(raw, &got); err != nil {
		return nil, err
	}

	item := got.AwemeDetail
	content := item.content()
	target := item.ShareURL
	if i := strings.IndexByte(target, '?'); i >= 0 {
		target = target[:i]
	}
	if target == "" {
		target = "https://www.douyin.com/video/" + awemeID
	}

	avatar := ""
	if len(item.Author.AvatarThumb.URLList) > 0 {
		avatar = item.Author.AvatarThumb.URLList[0]
	}
	return &model.Result{
		Platform:  platform,
		URL:       target,
		Timestamp: item.CreateTime,
		Author:    model.Author{Name: item.Author.Nickname, ID: item.Author.UID, Avatar: avatar, Desc: item.Author.Signature},
		Stats: model.Stats{
			Like:    htmlx.Num(item.Statistics.DiggCount),
			Comment: htmlx.Num(item.Statistics.CommentCount),
			Share:   htmlx.Num(item.Statistics.ShareCount),
			Collect: htmlx.Num(item.Statistics.CollectCount),
		},
		Content: content,
	}, nil
}

func (a aweme) content() []model.Content {
	out := []model.Content{model.Text{Value: strings.Replace(a.ShareInfo.ShareDescInfo, "#"+a.ShareInfo.ShareDesc+"#", "", 1)}}
	var musicURL string
	if a.Music != nil && !a.Music.IsOriginalSound {
		if a.Music.PlayURL.URI == "" {
			var extra struct {
				OriginalSongURL string `json:"original_song_url"`
			}
			if err := netx.Decode([]byte(a.Music.Extra), &extra); err == nil {
				musicURL = extra.OriginalSongURL
			}
		} else {
			musicURL = a.Music.PlayURL.URI
		}
		if musicURL != "" {
			out = append(out, model.Audio{URL: musicURL, Duration: float64(a.Music.Duration), Headers: headers})
		}
	}
	if len(a.Images) > 0 {
		for _, item := range a.Images {
			if item.ClipType == nil || *item.ClipType == 2 {
				if url := last(item.URLList); url != "" {
					out = append(out, model.Image{URL: url})
				}
			} else if item.Video != nil {
				out = append(out, model.LivePhoto{
					Image:   last(item.Video.Cover.URLList),
					Stream:  item.Video.PlayAddr.url(),
					Headers: headers,
				})
			}
		}
	} else if a.Video != nil {
		cover := last(a.Video.Cover.URLList)
		if a.Video.CoverOriginalScale != nil {
			cover = last(a.Video.CoverOriginalScale.URLList)
		}
		out = append(out, model.Video{
			URL:      a.Video.PlayAddr.url(),
			Cover:    cover,
			Duration: float64(a.Video.Duration / 1000),
			Headers:  headers,
		})
	}
	return out
}

func last(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[len(list)-1]
}

var errNoRoute = errorString("抖音短链没有解析出可用地址")

type errorString string

func (e errorString) Error() string { return string(e) }
