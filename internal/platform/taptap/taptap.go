package taptap

import (
	"context"
	"net/url"
	"strconv"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://www.taptap.cn/",
}

const (
	xUA        = "V=1&PN=WebApp&LANG=zh_CN&VN_CODE=93&VN=0.1.0&LOC=CN&PLT=PC"
	base       = "https://www.taptap.cn"
	detailPath = "/webapiv2/moment/v3/detail"
	videoPath  = "/webapiv2/video-resource/v1/multi-get"
)

var platform = model.Platform{Name: "taptap", DisplayName: "TapTap"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "www.taptap.cn",
			Pattern: parse.Compile(`moment/(?P<moment_id>\d+)`),
			Handle:  p.moment,
		},
		{
			Keyword: "www.taptap.cn",
			Pattern: parse.Compile(`explore/(?P<moment_id>\d+)`),
			Handle:  p.moment,
		},
	}
}

type img struct {
	OriginalURL string `json:"original_url"`
}

type info struct {
	Img   *img   `json:"img"`
	Style string `json:"style"`
}

type part struct {
	Type     string `json:"type"`
	Children []part `json:"children"`
	Text     string `json:"text"`
	Info     *info  `json:"info"`
}

type contents struct {
	JSON []part `json:"json"`
}

type user struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

type stat struct {
	Ups       int64 `json:"ups"`
	Comments  int64 `json:"comments"`
	Favorites int64 `json:"favorites"`
	Reposts   int64 `json:"reposts"`
	PvTotal   int64 `json:"pv_total"`
}

type topic struct {
	Title    string    `json:"title"`
	PinVideo *pinVideo `json:"pin_video"`
}

type pinVideo struct {
	VideoID int64 `json:"video_id"`
}

type moment struct {
	IDStr       string `json:"id_str"`
	CreatedTime int64  `json:"created_time"`
	EditedTime  int64  `json:"edited_time"`
	Author      struct {
		User user `json:"user"`
	} `json:"author"`
	Topic topic `json:"topic"`
	Stat  stat  `json:"stat"`
}

type firstPost struct {
	IDStr        string    `json:"id_str"`
	Contents     *contents `json:"contents"`
	FooterImages []img     `json:"footer_images"`
}

type momentResponse struct {
	Data struct {
		Moment    moment    `json:"moment"`
		FirstPost firstPost `json:"first_post"`
	} `json:"data"`
}

type video struct {
	VideoID int64 `json:"video_id"`
	PlayURL struct {
		URL string `json:"url"`
	} `json:"play_url"`
	Info struct {
		Duration int64 `json:"duration"`
	} `json:"info"`
	RawCover struct {
		URL string `json:"url"`
	} `json:"raw_cover"`
}

type videoResponse struct {
	Data struct {
		List []video `json:"list"`
	} `json:"data"`
}

func (p *Parser) moment(ctx context.Context, m *parse.Match) (*model.Result, error) {
	momentID := m.Get("moment_id")
	var got momentResponse
	if err := p.client.GetJSON(base+detailPath+"?"+query(momentID), &got, nil); err != nil {
		return nil, err
	}

	content := buildContent(got.Data.FirstPost)
	if pin := got.Data.Moment.Topic.PinVideo; pin != nil {
		var videos videoResponse
		if err := p.client.GetJSON(base+videoPath+"?"+videoQuery(strconv.FormatInt(pin.VideoID, 10)), &videos, nil); err != nil {
			return nil, err
		}
		for _, item := range videos.Data.List {
			content = append(content, model.Video{
				URL:      item.PlayURL.URL,
				Cover:    item.RawCover.URL,
				Duration: float64(item.Info.Duration),
				Headers:  headers,
			})
		}
	}

	author := got.Data.Moment.Author.User
	return &model.Result{
		Platform: platform,
		URL:      "https://www.taptap.com/moment/" + got.Data.Moment.IDStr,
		Title:    got.Data.Moment.Topic.Title,
		Author: model.Author{
			Name:   author.Name,
			ID:     strconv.FormatInt(author.ID, 10),
			Avatar: author.Avatar,
		},
		Stats: model.Stats{
			View:    htmlx.Num(got.Data.Moment.Stat.PvTotal),
			Like:    htmlx.Num(got.Data.Moment.Stat.Ups),
			Share:   htmlx.Num(got.Data.Moment.Stat.Reposts),
			Comment: htmlx.Num(got.Data.Moment.Stat.Comments),
			Collect: htmlx.Num(got.Data.Moment.Stat.Favorites),
		},
		Timestamp: got.Data.Moment.CreatedTime,
		Content:   content,
	}, nil
}

func buildContent(post firstPost) []model.Content {
	out := make([]model.Content, 0, len(post.FooterImages)+1)
	if post.Contents != nil {
		for _, jpart := range post.Contents.JSON {
			appendPart(jpart, &out)
			if jpart.Type == "paragraph" {
				out = append(out, model.Text{Value: "\n"})
			}
		}
	}
	for _, footer := range post.FooterImages {
		if footer.OriginalURL != "" {
			out = append(out, model.Image{URL: footer.OriginalURL})
		}
	}
	return out
}

func appendPart(item part, out *[]model.Content) {
	var picture *img
	if item.Info != nil {
		picture = item.Info.Img
	}
	if item.Type == "tap_emoji" {
		desc := ""
		for _, child := range item.Children {
			if child.Text != "" {
				desc = child.Text
				break
			}
		}
		if picture != nil {
			*out = append(*out, model.Image{URL: picture.OriginalURL})
		} else if desc != "" || item.Text != "" {
			text := desc
			if text == "" {
				text = item.Text
			}
			*out = append(*out, model.Text{Value: text})
		}
		return
	}
	if item.Text != "" {
		*out = append(*out, model.Text{Value: item.Text})
	}
	if picture != nil {
		*out = append(*out, model.Image{URL: picture.OriginalURL})
	}
	for _, child := range item.Children {
		appendPart(child, out)
	}
}

func query(momentID string) string {
	values := url.Values{}
	values.Set("id", momentID)
	values.Set("X-UA", xUA)
	return values.Encode()
}

func videoQuery(videoID string) string {
	values := url.Values{}
	values.Set("video_ids", videoID)
	values.Set("X-UA", xUA)
	return values.Encode()
}
