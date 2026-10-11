package lofter

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
	"User-Agent": "Mozilla/5.0 (Linux; Android 16; 2201123C Build/1450000) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/121.0.6167.71 Safari/537.36 Mobile Safari/537.36 V1_AND_SQ_9.3.70_16410_YYB_D QQ/9.3.70.41925 AppId/537395443",
}

const detailAPI = "https://api.lofter.com/oldapi/post/detail.api?product=lofter-android-8.1.20"

var platform = model.Platform{Name: "lofter", DisplayName: "LOFTER"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "s.lofter.com",
			Pattern: parse.Compile(`s\.lofter\.com/-s/[0-9A-Za-z]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "lofter.com",
			Pattern: parse.Compile(`post/(?P<blog_hex>[0-9a-zA-Z]+)_(?P<post_hex>[0-9a-zA-Z]+)`),
			Handle:  p.post,
		},
	}
}

type blogInfo struct {
	BlogID       int64  `json:"blogId"`
	BlogName     string `json:"blogName"`
	BlogNickName string `json:"blogNickName"`
	BigAvaImg    string `json:"bigAvaImg"`
	HomePageURL  string `json:"homePageUrl"`
}

type postCount struct {
	ResponseCount int64 `json:"responseCount"`
	FavoriteCount int64 `json:"favoriteCount"`
	ShareCount    int64 `json:"shareCount"`
	ReblogCount   int64 `json:"reblogCount"`
	PostHot       int64 `json:"postHot"`
}

type post struct {
	Type        int64     `json:"type"`
	BlogID      int64     `json:"blogId"`
	Title       string    `json:"title"`
	PublishTime int64     `json:"publishTime"`
	Tag         string    `json:"tag"`
	TagList     []string  `json:"tagList"`
	Content     string    `json:"content"`
	BlogInfo    blogInfo  `json:"blogInfo"`
	PostCount   postCount `json:"postCount"`
	IPLocation  string    `json:"ipLocation"`
	PhotoLinks  string    `json:"photoLinks"`
}

type envelope struct {
	Meta struct {
		Status int64  `json:"status"`
		Msg    string `json:"msg"`
	} `json:"meta"`
	Response struct {
		Posts []struct {
			Post post `json:"post"`
		} `json:"posts"`
	} `json:"response"`
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

func (p *Parser) post(ctx context.Context, m *parse.Match) (*model.Result, error) {
	blogID, err := strconv.ParseInt(m.Get("blog_hex"), 16, 64)
	if err != nil {
		return nil, err
	}
	postID, err := strconv.ParseInt(m.Get("post_hex"), 16, 64)
	if err != nil {
		return nil, err
	}

	raw, err := p.client.PostForm(detailAPI, map[string][]string{
		"postid":       {strconv.FormatInt(postID, 10)},
		"targetblogid": {strconv.FormatInt(blogID, 10)},
	}, nil)
	if err != nil {
		return nil, err
	}
	var got envelope
	if err := netx.Decode(raw, &got); err != nil {
		return nil, err
	}
	if got.Meta.Status != 200 {
		return nil, errBadResponse
	}
	if len(got.Response.Posts) == 0 {
		return nil, errNoPost
	}
	item := got.Response.Posts[0].Post

	content := make([]model.Content, 0, 4)
	if text := htmlx.Text(item.Content); text != "" {
		content = append(content, model.Text{Value: text})
	}
	var photos []struct {
		Orign string `json:"orign"`
	}
	if item.PhotoLinks != "" {
		if err := netx.Decode([]byte(item.PhotoLinks), &photos); err == nil {
			for _, photo := range photos {
				if photo.Orign != "" {
					content = append(content, model.Image{URL: photo.Orign})
				}
			}
		}
	}

	return &model.Result{
		Platform:  platform,
		URL:       "https://" + item.BlogInfo.BlogName + ".lofter.com/" + m.URL,
		Title:     item.Title,
		Timestamp: item.PublishTime / 1000,
		Author: model.Author{
			Name:   item.BlogInfo.BlogNickName,
			ID:     item.BlogInfo.BlogName,
			Avatar: item.BlogInfo.BigAvaImg,
		},
		Stats: model.Stats{
			Like:    htmlx.Num(item.PostCount.FavoriteCount),
			Share:   htmlx.Num(item.PostCount.ShareCount),
			Comment: htmlx.Num(item.PostCount.ResponseCount),
		},
		Content: content,
	}, nil
}

var (
	errNoRoute     = errorString("Lofter 短链没有解析出可用地址")
	errBadResponse = errorString("Lofter 解析失败: 未知错误")
	errNoPost      = errorString("Lofter 解析失败: 未找到帖子内容")
)

type errorString string

func (e errorString) Error() string { return string(e) }
