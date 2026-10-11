package duitang

import (
	"context"
	"net/http"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Referer":    "https://www.duitang.com/",
}

const (
	atlasAPI = "https://www.duitang.com/napi/vienna/atlas/detail/"
	blogAPI  = "https://www.duitang.com/napi/blog/with_instance_tag/detail/"
)

var platform = model.Platform{Name: "duitang", DisplayName: "堆糖"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "duitang.com/blog",
			Pattern: parse.Compile(`duitang\.com/blog[^\s]*`),
			Params:  map[string]parse.ParamRule{"id": {AsInt: true}},
			Handle:  p.blog,
		},
		{
			Keyword: "duitang.com/atlas",
			Pattern: parse.Compile(`duitang\.com/atlas[^\s]*`),
			Params:  map[string]parse.ParamRule{"id": {AsInt: true}},
			Handle:  p.atlas,
		},
	}
}

type sender struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
}

type envelope struct {
	Status int64 `json:"status"`
	Data   struct {
		ID            int64  `json:"id"`
		Msg           string `json:"msg"`
		Desc          string `json:"desc"`
		Sender        sender `json:"sender"`
		LikeCount     int64  `json:"like_count"`
		FavoriteCount int64  `json:"favorite_count"`
		ReplyCount    int64  `json:"reply_count"`
		CommentCount  int64  `json:"comment_count"`
		VisitCount    int64  `json:"visit_count"`
		AddDatetimeTS int64  `json:"add_datetime_ts"`
		CreatedAt     int64  `json:"created_at"`
		Photo         struct {
			Path string `json:"path"`
		} `json:"photo"`
		Blogs []struct {
			Photo struct {
				Path string `json:"path"`
			} `json:"photo"`
		} `json:"blogs"`
	} `json:"data"`
}

func (p *Parser) blog(ctx context.Context, m *parse.Match) (*model.Result, error) {
	var got envelope
	url := blogAPI + "?blog_id=" + m.Get("id") + "&include_fields=tags,related_albums,related_albums.covers,root_album,share_links_2,extra_links,icon_description,root_id"
	if err := p.client.GetJSON(url, &got, nil); err != nil {
		return nil, err
	}
	if got.Status != 1 {
		return nil, errBadResponse
	}

	content := make([]model.Content, 0, 2)
	if text := htmlx.Compact(got.Data.Msg); text != "" {
		content = append(content, model.Text{Value: text})
	}
	if path := got.Data.Photo.Path; path != "" {
		content = append(content, model.Image{URL: path})
	}

	return &model.Result{
		Platform:  platform,
		URL:       "https://m.duitang.com/blog?id=" + m.Get("id"),
		Author:    author(got.Data.Sender),
		Timestamp: got.Data.AddDatetimeTS,
		Content:   content,
		Stats: model.Stats{
			Like:    htmlx.Num(got.Data.LikeCount),
			Collect: htmlx.Num(got.Data.FavoriteCount),
			Comment: htmlx.Num(got.Data.ReplyCount),
		},
	}, nil
}

func (p *Parser) atlas(ctx context.Context, m *parse.Match) (*model.Result, error) {
	var got envelope
	if err := p.client.GetJSON(atlasAPI+"?atlas_id="+m.Get("id"), &got, nil); err != nil {
		return nil, err
	}
	if got.Status != 1 {
		return nil, errBadResponse
	}

	content := make([]model.Content, 0, len(got.Data.Blogs)+1)
	if text := htmlx.Compact(got.Data.Desc); text != "" {
		content = append(content, model.Text{Value: text})
	}
	for _, blog := range got.Data.Blogs {
		if path := blog.Photo.Path; path != "" {
			content = append(content, model.Image{URL: path})
		}
	}

	return &model.Result{
		Platform:  platform,
		URL:       "https://www.duitang.com/atlas?id=" + m.Get("id"),
		Author:    author(got.Data.Sender),
		Timestamp: got.Data.CreatedAt / 1000,
		Content:   content,
		Stats: model.Stats{
			View:    htmlx.Num(got.Data.VisitCount),
			Like:    htmlx.Num(got.Data.LikeCount),
			Collect: htmlx.Num(got.Data.FavoriteCount),
			Comment: htmlx.Num(got.Data.CommentCount),
		},
	}, nil
}

func author(s sender) model.Author {
	return model.Author{Name: s.Username, ID: itoa(s.ID), Avatar: s.Avatar}
}

func itoa(v int64) string {
	if v == 0 {
		return ""
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

var errBadResponse = errorString("堆糖接口返回异常")

type errorString string

func (e errorString) Error() string { return string(e) }

var _ = http.MethodGet
