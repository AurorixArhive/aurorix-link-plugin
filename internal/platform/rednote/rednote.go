package rednote

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent":       "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1 Edg/132.0.0.0",
	"Referer":          "https://www.xiaohongshu.com/",
	"Origin":           "https://www.xiaohongshu.com",
	"X-Requested-With": "XMLHttpRequest",
}

var initialStateRe = regexp.MustCompile(`(?s)window\.__INITIAL_STATE__=(.*?)</script>`)

var platform = model.Platform{Name: "rednote", DisplayName: "小红书"}

type Parser struct {
	client *netx.Client
	cookie string
}

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) SetCookie(value string) { p.cookie = strings.TrimSpace(value) }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "xhslink.cn",
			Pattern: parse.Compile(`xhslink\.cn/[A-Za-z0-9._?%&+=/#@-]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "xhslink.com",
			Pattern: parse.Compile(`xhslink\.com/[A-Za-z0-9._?%&+=/#@-]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "xiaohongshu.com",
			Pattern: parse.Compile(`(?P<type>explore|search_result|discovery/item)/(?P<note_id>[0-9a-zA-Z]+)[^\s]*`),
			Params:  map[string]parse.ParamRule{"xsec_token": {}},
			Handle:  p.note,
		},
	}
}

type streamURL struct {
	MasterURL string `json:"masterUrl"`
}

type stream struct {
	H264 []streamURL `json:"h264"`
	H265 []streamURL `json:"h265"`
	H266 []streamURL `json:"h266"`
	AV1  []streamURL `json:"av1"`
}

func (s stream) url() string {
	for _, list := range [][]streamURL{s.H264, s.H265, s.H266, s.AV1} {
		if len(list) > 0 {
			return list[0].MasterURL
		}
	}
	return ""
}

type noteImage struct {
	FileID    string `json:"fileId"`
	LivePhoto bool   `json:"livePhoto"`
	Stream    stream `json:"stream"`
}

type noteVideo struct {
	Capa struct {
		Duration float64 `json:"duration"`
	} `json:"capa"`
	Consumer struct {
		OriginVideoKey string `json:"originVideoKey"`
	} `json:"consumer"`
}

type noteDetail struct {
	Title string `json:"title"`
	Desc  string `json:"desc"`
	User  struct {
		NickName string `json:"nickName"`
		Avatar   string `json:"avatar"`
		UserID   string `json:"userId"`
	} `json:"user"`
	LastUpdateTime int64 `json:"lastUpdateTime"`
	InteractInfo   struct {
		LikedCount     string `json:"likedCount"`
		CollectedCount string `json:"collectedCount"`
		CommentCount   string `json:"commentCount"`
		ShareCount     string `json:"shareCount"`
	} `json:"interactInfo"`
	Cover struct {
		FileID string `json:"fileId"`
	} `json:"cover"`
	ImageList []noteImage `json:"imageList"`
	Video     *noteVideo  `json:"video"`
}

type initialState struct {
	NoteData struct {
		Data struct {
			NoteData noteDetail `json:"noteData"`
		} `json:"data"`
	} `json:"noteData"`
}

func (p *Parser) requestHeaders() map[string]string {
	if p.cookie == "" {
		return headers
	}
	merged := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		merged[k] = v
	}
	merged["Cookie"] = p.cookie
	return merged
}

func (p *Parser) shortLink(ctx context.Context, m *parse.Match) (*model.Result, error) {
	target := m.URL
	if !strings.HasPrefix(target, "http") {
		target = "https://" + target
	}
	final, err := p.client.FinalURL(target, p.requestHeaders())
	if err != nil {
		return nil, err
	}
	if final == target {
		return nil, errNoRedirect
	}
	for _, route := range p.Routes() {
		if route.Keyword == "xhslink.cn" || route.Keyword == "xhslink.com" {
			continue
		}
		if match := matchRoute(route, final); match != nil {
			return route.Handle(ctx, match)
		}
	}
	return nil, errNoRoute
}

func (p *Parser) note(ctx context.Context, m *parse.Match) (*model.Result, error) {
	noteID := m.Get("note_id")
	token := m.Get("xsec_token")
	url := "https://www.xiaohongshu.com/discovery/item/" + noteID + "?xsec_token=" + token + "&xsec_source=pc_share"

	page, err := p.client.Get(url, p.requestHeaders())
	if err != nil {
		return nil, err
	}
	found := initialStateRe.FindSubmatch(page)
	if found == nil {
		return nil, errNoState
	}
	raw := strings.ReplaceAll(string(found[1]), "undefined", "null")

	var state initialState
	if err := netx.Decode([]byte(raw), &state); err != nil {
		return nil, err
	}
	detail := state.NoteData.Data.NoteData
	if detail.User.UserID == "" && detail.Title == "" && detail.Video == nil && len(detail.ImageList) == 0 {
		return nil, errNoData
	}

	content := make([]model.Content, 0, len(detail.ImageList)+2)
	if text := htmlx.Compact(detail.Desc); text != "" {
		content = append(content, model.Text{Value: text})
	}
	content = append(content, detail.medias()...)

	return &model.Result{
		Platform: platform,
		URL:      url,
		Title:    detail.Title,
		Author: model.Author{
			Name:   detail.User.NickName,
			ID:     detail.User.UserID,
			Avatar: detail.User.Avatar,
		},
		Stats: model.Stats{
			Like:    detail.InteractInfo.LikedCount,
			Comment: detail.InteractInfo.CommentCount,
			Share:   detail.InteractInfo.ShareCount,
			Collect: detail.InteractInfo.CollectedCount,
		},
		Timestamp: detail.LastUpdateTime / 1000,
		Content:   content,
	}, nil
}

func (d noteDetail) medias() []model.Content {
	out := make([]model.Content, 0, len(d.ImageList)+1)
	if d.Video != nil {
		out = append(out, model.Video{
			URL:      "https://sns-video-bd.xhscdn.com/" + d.Video.Consumer.OriginVideoKey,
			Cover:    imageURL(d.Cover.FileID),
			Duration: d.Video.Capa.Duration,
			Headers:  headers,
		})
		return out
	}
	for _, img := range d.ImageList {
		if img.LivePhoto {
			out = append(out, model.LivePhoto{
				Image:   imageURL(img.FileID),
				Stream:  img.Stream.url(),
				Headers: headers,
			})
			continue
		}
		if url := imageURL(img.FileID); url != "" {
			out = append(out, model.Image{URL: url})
		}
	}
	return out
}

func imageURL(fileID string) string {
	if fileID == "" {
		return ""
	}
	return "https://ci.xiaohongshu.com/" + fileID + "?imageView2/2/w/1080/format/jpg"
}

func matchRoute(route parse.Route, raw string) *parse.Match {
	groups := route.Pattern.FindStringSubmatch(raw)
	if groups == nil {
		return nil
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
		if i := strings.IndexByte(parsed.Fragment, '?'); i >= 0 {
			if values, err := url.ParseQuery(parsed.Fragment[i+1:]); err == nil {
				for key, list := range values {
					if len(list) > 0 && params[key] == "" {
						params[key] = list[0]
					}
				}
			}
		}
	}
	for name, rule := range route.Params {
		value, has := params[name]
		if !has && rule.Default != "" {
			value, has = rule.Default, true
			params[name] = value
		}
		if !has {
			if rule.Optional {
				continue
			}
			return nil
		}
		if rule.Equals != "" && value != rule.Equals {
			return nil
		}
		if len(rule.OneOf) > 0 && !contains(rule.OneOf, value) {
			return nil
		}
		if rule.AsInt {
			if _, err := strconv.Atoi(value); err != nil {
				return nil
			}
		}
	}
	return &parse.Match{URL: groups[0], Params: params}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

var (
	errNoState    = errorString("小红书页面里没有 __INITIAL_STATE__")
	errNoData     = errorString("小红书笔记数据为空, 可能链接失效或需要 Cookie")
	errNoRedirect = errorString("小红书短链没有跳转到笔记页")
	errNoRoute    = errorString("小红书短链没有解析出可用地址")
)

type errorString string

func (e errorString) Error() string { return string(e) }

func init() { parse.Register(New) }
