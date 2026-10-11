package ds

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://ds.163.com/",
}

const facadeAPI = "https://inf.ds.163.com/v1/web/feed/basic/facade?feedId="

var platform = model.Platform{Name: "ds", DisplayName: "大神"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "ds.163.com",
			Pattern: parse.Compile(`article/(?P<feed_id>[A-Za-z0-9]+)`),
			Handle:  p.feed,
		},
		{
			Keyword: "ds.163.com",
			Pattern: parse.Compile(`feed/(?P<feed_id>[A-Za-z0-9]+)`),
			Handle:  p.feed,
		},
	}
}

type user struct {
	UID   string `json:"uid"`
	Nick  string `json:"nick"`
	Icon  string `json:"icon"`
	Intro string `json:"intro"`
}

type record struct {
	RepostCount  int64 `json:"repostCount"`
	CommentCount int64 `json:"commentCount"`
	LikeCount    int64 `json:"likeCount"`
	ShareCount   int64 `json:"shareCount"`
	FavCount     int64 `json:"favCount"`
}

type media struct {
	URL      string `json:"url"`
	MimeType string `json:"mimeType"`
	Duration int64  `json:"duration"`
	Cover    string `json:"cover"`
}

type body struct {
	Text     string  `json:"text"`
	Media    []media `json:"media"`
	Title    string  `json:"title"`
	LongText string  `json:"longText"`
}

type feed struct {
	ID            string `json:"id"`
	UID           string `json:"uid"`
	Dumps         string `json:"content"`
	CreateTime    int64  `json:"createTime"`
	Record        record `json:"record"`
	DisplayIPInfo *struct {
		IPLocationName string `json:"ipLocationName"`
	} `json:"displayIpInfo"`
}

type facade struct {
	Code   int64 `json:"code"`
	Result struct {
		Feed      feed `json:"feed"`
		UserInfos []struct {
			User user `json:"user"`
		} `json:"userInfos"`
	} `json:"result"`
}

func (p *Parser) feed(ctx context.Context, m *parse.Match) (*model.Result, error) {
	feedID := m.Get("feed_id")
	var got facade
	if err := p.client.GetJSON(facadeAPI+feedID, &got, nil); err != nil {
		return nil, err
	}
	if got.Code != 200 {
		return nil, errCode
	}

	item := got.Result.Feed
	var decoded struct {
		Body body `json:"body"`
	}
	if item.Dumps != "" {
		if err := json.Unmarshal([]byte(item.Dumps), &decoded); err != nil {
			return nil, err
		}
	}

	author := model.Author{}
	for _, info := range got.Result.UserInfos {
		if info.User.UID == item.UID {
			author = model.Author{Name: info.User.Nick, ID: info.User.UID, Avatar: info.User.Icon, Desc: info.User.Intro}
			break
		}
	}

	return &model.Result{
		Platform:  platform,
		URL:       "https://ds.163.com/feed/" + feedID,
		Title:     decoded.Body.Title,
		Author:    author,
		Timestamp: item.CreateTime / 1000,
		Content:   build(decoded.Body),
		Stats: model.Stats{
			Like:    htmlx.Num(item.Record.LikeCount),
			Share:   htmlx.Num(item.Record.ShareCount),
			Comment: htmlx.Num(item.Record.CommentCount),
			Collect: htmlx.Num(item.Record.FavCount),
		},
	}, nil
}

func build(item body) []model.Content {
	if item.LongText != "" {
		rich := htmlx.Rich(item.LongText)
		content := make([]model.Content, 0, len(rich.Images)+1)
		if rich.Text != "" {
			content = append(content, model.Text{Value: rich.Text})
		}
		for _, url := range rich.Images {
			content = append(content, model.Image{URL: url})
		}
		return content
	}

	content := make([]model.Content, 0, len(item.Media)+1)
	if text := htmlx.Compact(item.Text); text != "" {
		content = append(content, model.Text{Value: text})
	}
	for _, m := range item.Media {
		switch {
		case strings.HasPrefix(m.MimeType, "video"):
			content = append(content, model.Video{
				URL:      m.URL,
				Cover:    m.Cover,
				Duration: float64(m.Duration / 1000),
				Headers:  headers,
			})
		case strings.HasPrefix(m.MimeType, "image"):
			content = append(content, model.Image{URL: m.URL})
		}
	}
	return content
}

var errCode = errors.New("大神接口返回非 200")

func init() { parse.Register(New) }
