package fiveeplay

import (
	"context"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://csgo.5eplay.com/",
}

const topicAPI = "https://app.5eplay.com/api/csgo/forum/topic/"

var platform = model.Platform{Name: "fiveeplay", DisplayName: "5EPlay"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "csgo.5eplay.com/forum",
			Pattern: parse.Compile(`forum/(?P<topic_id>\d+)`),
			Handle:  p.topic,
		},
		{
			Keyword: "csgo.5eplay.com/forum",
			Pattern: parse.Compile(`share/(?P<topic_id>\d+)`),
			Handle:  p.topic,
		},
	}
}

type post struct {
	Tid             string   `json:"tid"`
	Type            string   `json:"type"`
	Domain          string   `json:"domain"`
	Username        string   `json:"username"`
	AvatarURL       string   `json:"avatar_url"`
	Title           string   `json:"title"`
	Dateline        string   `json:"dateline"`
	TopicLikesCount string   `json:"topic_likes_count"`
	Images          []string `json:"images"`
	Hits            string   `json:"hits"`
	IntroText       string   `json:"intro_text"`
	Forward         string   `json:"forward"`
	TopicFavorites  string   `json:"topic_favorites"`
	ShareData       struct {
		ShareURL string `json:"share_url"`
	} `json:"share_data"`
	VideoData struct {
		VideoCover string `json:"video_cover"`
		VideoURL   string `json:"video_url"`
	} `json:"video_data"`
}

type response struct {
	Success bool  `json:"success"`
	Errcode int64 `json:"errcode"`
	Data    struct {
		Content  post `json:"content"`
		Comments struct {
			Total int64 `json:"total"`
		} `json:"comments"`
	} `json:"data"`
}

func (p *Parser) topic(ctx context.Context, m *parse.Match) (*model.Result, error) {
	var got response
	if err := p.client.GetJSON(topicAPI+m.Get("topic_id"), &got, nil); err != nil {
		return nil, err
	}
	item := got.Data.Content

	content := make([]model.Content, 0, len(item.Images)+2)
	if text := strings.TrimSpace(strings.Split(item.IntroText, "<img")[0]); text != "" {
		content = append(content, model.Text{Value: text})
	}
	for _, image := range item.Images {
		if image != "" {
			content = append(content, model.Image{URL: image})
		}
	}
	if item.VideoData.VideoURL != "" {
		content = append(content, model.Video{
			URL:     item.VideoData.VideoURL,
			Cover:   item.VideoData.VideoCover,
			Headers: headers,
		})
	}

	timestamp, _ := strconv.ParseInt(item.Dateline, 10, 64)
	return &model.Result{
		Platform:  platform,
		URL:       item.ShareData.ShareURL,
		Title:     item.Title,
		Timestamp: timestamp,
		Author: model.Author{
			Name:   item.Username,
			ID:     item.Domain,
			Avatar: item.AvatarURL,
		},
		Stats: model.Stats{
			Like:    item.TopicLikesCount,
			Share:   item.Forward,
			Comment: htmlx.Num(got.Data.Comments.Total),
			Collect: item.TopicFavorites,
			View:    item.Hits,
		},
		Content: content,
	}, nil
}
