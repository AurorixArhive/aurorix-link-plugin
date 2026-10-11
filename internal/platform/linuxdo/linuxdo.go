package linuxdo

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Referer":    "https://linux.do/",
}

var platform = model.Platform{Name: "linuxdo", DisplayName: "LINUX DO"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) SetCookie(value string) { p.client.SetCookie(value) }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{{
		Keyword: "linux.do",
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
	PostStream struct {
		Posts []post `json:"posts"`
	} `json:"post_stream"`
	Title      string `json:"title"`
	ID         int64  `json:"id"`
	PostsCount int64  `json:"posts_count"`
	ReplyCount int64  `json:"reply_count"`
	Views      int64  `json:"views"`
	LikeCount  int64  `json:"like_count"`
}

func (p *Parser) topic(ctx context.Context, m *parse.Match) (*model.Result, error) {
	topicID := m.Get("topic_id")
	var got response
	if err := p.client.GetJSON("https://linux.do/t/topic/"+topicID+".json", &got, nil); err != nil {
		return nil, err
	}
	if len(got.PostStream.Posts) == 0 {
		return nil, errNoPost
	}

	detail := got.PostStream.Posts[0]
	content := make([]model.Content, 0, 4)
	if media := htmlx.Rich(detail.Cooked); media.Text != "" || len(media.Images) > 0 {
		if media.Text != "" {
			content = append(content, model.Text{Value: media.Text})
		}
		for _, image := range media.Images {
			content = append(content, model.Image{URL: image})
		}
	}

	name := detail.DisplayUsername
	if name == "" {
		name = detail.Username
	}
	return &model.Result{
		Platform: platform,
		URL:      "https://linux.do/t/topic/" + strconv.FormatInt(got.ID, 10),
		Title:    got.Title,
		Author: model.Author{
			Name:   name,
			Avatar: avatarURL(detail.AvatarTemplate),
		},
		Stats: model.Stats{
			Like:    htmlx.Num(got.LikeCount),
			View:    htmlx.Num(got.Views),
			Comment: htmlx.Num(got.PostsCount - 1),
		},
		Timestamp: htmlx.Time(detail.CreatedAt),
		Content:   content,
	}, nil
}

func avatarURL(template string) string {
	if template == "" {
		return ""
	}
	template = strings.ReplaceAll(template, "{size}", "288")
	base, err := url.Parse("https://linux.do/")
	if err != nil {
		return template
	}
	ref, err := url.Parse(template)
	if err != nil {
		return template
	}
	return base.ResolveReference(ref).String()
}

var errNoPost = errorString("LINUX DO 帖子没有内容")

type errorString string

func (e errorString) Error() string { return string(e) }
