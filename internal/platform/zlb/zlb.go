package zlb

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://bb.zlb.ink/",
}

const topicAPI = "https://bb.zlb.ink/t/topic/"

var platform = model.Platform{Name: "zlb", DisplayName: "壁吧专楼吧"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{{
		Keyword: "zlb.ink",
		Pattern: parse.Compile(`topic/(?P<topic_id>\d+)`),
		Handle:  p.topic,
	}}
}

type post struct {
	Username           string `json:"username"`
	DisplayUsername    string `json:"display_username"`
	CreatedAt          string `json:"created_at"`
	Cooked             string `json:"cooked"`
	AvatarTemplate     string `json:"avatar_template"`
	ReplyCount         int64  `json:"reply_count"`
	ReactionUsersCount int64  `json:"reaction_users_count"`
}

type response struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	PostsCount int64  `json:"posts_count"`
	Views      int64  `json:"views"`
	LikeCount  int64  `json:"like_count"`
	PostStream struct {
		Posts []post `json:"posts"`
	} `json:"post_stream"`
}

func (p *Parser) topic(ctx context.Context, m *parse.Match) (*model.Result, error) {
	topicID := m.Get("topic_id")
	var got response
	if err := p.client.GetJSON(topicAPI+topicID+".json", &got, nil); err != nil {
		return nil, err
	}
	if len(got.PostStream.Posts) == 0 {
		return nil, errEmpty
	}

	detail := got.PostStream.Posts[0]
	rich := htmlx.Rich(detail.Cooked)
	content := make([]model.Content, 0, len(rich.Images)+1)
	if rich.Text != "" {
		content = append(content, model.Text{Value: rich.Text})
	}
	for _, url := range rich.Images {
		content = append(content, model.Image{URL: url})
	}

	name := detail.DisplayUsername
	if name == "" {
		name = detail.Username
	}

	return &model.Result{
		Platform: platform,
		URL:      "https://bb.zlb.ink/t/topic/" + strconv.FormatInt(got.ID, 10),
		Title:    got.Title,
		Author:   model.Author{Name: name, Avatar: avatar(detail.AvatarTemplate)},
		Stats: model.Stats{
			View:    htmlx.Num(got.Views),
			Like:    htmlx.Num(got.LikeCount),
			Comment: htmlx.Num(got.PostsCount - 1),
		},
		Timestamp: htmlx.Time(detail.CreatedAt),
		Content:   content,
	}, nil
}

func avatar(template string) string {
	if template == "" {
		return ""
	}
	template = strings.Replace(template, "{size}", "288", 1)
	if strings.HasPrefix(template, "http") {
		return template
	}
	return "https://zlb.ink/" + strings.TrimPrefix(template, "/")
}

var errEmpty = errors.New("壁吧专楼吧帖子没有内容")

func init() { parse.Register(New) }
