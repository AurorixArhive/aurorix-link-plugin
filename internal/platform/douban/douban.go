package douban

import (
	"context"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Referer":    "https://m.douban.com/",
}

const topicAPI = "https://m.douban.com/rexxar/api/v2/group/topic/"

var platform = model.Platform{Name: "douban", DisplayName: "豆瓣"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{{
		Keyword: "douban.com/group/topic/",
		Pattern: parse.Compile(`topic/(?P<topic_id>\d+)`),
		Handle:  p.topic,
	}}
}

type post struct {
	URL           string `json:"url"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	CreateTime    string `json:"create_time"`
	UpdateTime    string `json:"update_time"`
	IPLocation    string `json:"ip_location"`
	ResharesCount int64  `json:"reshares_count"`
	LikeCount     int64  `json:"like_count"`
	CommentsCount int64  `json:"comments_count"`
	ImageLayout   string `json:"image_layout"`
	Author        struct {
		Name   string `json:"name"`
		UID    string `json:"uid"`
		Avatar string `json:"avatar"`
	} `json:"author"`
	Photos []struct {
		Image struct {
			Large struct {
				URL string `json:"url"`
			} `json:"large"`
		} `json:"image"`
	} `json:"photos"`
}

func (p *Parser) topic(ctx context.Context, m *parse.Match) (*model.Result, error) {
	var got post
	err := p.client.GetJSON(topicAPI+m.Get("topic_id"), &got, nil)
	if err != nil {
		return nil, err
	}

	rich := htmlx.Rich(got.Content)
	content := make([]model.Content, 0, len(got.Photos)+1)
	if rich.Text != "" {
		content = append(content, model.Text{Value: rich.Text})
	}
	if got.ImageLayout == "horizontal" {
		for _, photo := range got.Photos {
			if url := photo.Image.Large.URL; url != "" {
				content = append(content, model.Image{URL: url})
			}
		}
	}

	url := got.URL
	if url == "" {
		url = "https://www.douban.com/group/topic/" + m.Get("topic_id")
	}

	result := &model.Result{
		Platform: platform,
		URL:      url,
		Title:    got.Title,
		Author: model.Author{
			Name:   got.Author.Name,
			ID:     got.Author.UID,
			Avatar: got.Author.Avatar,
		},
		Stats: model.Stats{
			Like:    htmlx.Num(got.LikeCount),
			Share:   htmlx.Num(got.ResharesCount),
			Comment: htmlx.Num(got.CommentsCount),
		},
		Content: content,
	}
	result.Timestamp = htmlx.Time(got.UpdateTime)
	if result.Timestamp == 0 {
		result.Timestamp = htmlx.Time(got.CreateTime)
	}
	return result, nil
}

func init() { parse.Register(New) }
