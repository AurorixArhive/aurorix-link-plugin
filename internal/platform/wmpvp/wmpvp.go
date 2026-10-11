package wmpvp

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
	"Referer":    "https://news.wmpvp.com/",
}

const (
	postAPI = "https://appengine.wmpvp.com/steamcn/community/post/getPostById?postId="
	newsAPI = "https://appactivity.wmpvp.com/steamcn/app/news/getAppNewsById"
)

var platform = model.Platform{Name: "wmpvp", DisplayName: "完美世界电竞"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "news.wmpvp.com/community-detail.html",
			Pattern: parse.Domain("news.wmpvp.com/community-detail.html"),
			Params:  map[string]parse.ParamRule{"id": {AsInt: true}},
			Handle:  p.post,
		},
		{
			Keyword: "news.wmpvp.com/community-pcDetail.html",
			Pattern: parse.Domain("news.wmpvp.com/community-pcDetail.html"),
			Params:  map[string]parse.ParamRule{"id": {AsInt: true}},
			Handle:  p.post,
		},
		{
			Keyword: "news.wmpvp.com/news.html",
			Pattern: parse.Domain("news.wmpvp.com/news.html"),
			Params:  map[string]parse.ParamRule{"id": {AsInt: true}, "gameTypeStr": {AsInt: true}},
			Handle:  p.news,
		},
	}
}

type communityUser struct {
	UserID   int64  `json:"userId"`
	SteamID  string `json:"steamId"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
}

type image struct {
	URL string `json:"url"`
}

type playInfo struct {
	PlayURL string `json:"playURL"`
	Format  string `json:"format"`
}
type videoInfo struct {
	VideoBase struct {
		CoverURL string `json:"coverURL"`
		Duration string `json:"duration"`
		VideoID  string `json:"videoId"`
	} `json:"videoBase"`
	PlayInfoList []playInfo `json:"playInfoList"`
}

type post struct {
	ID             int64         `json:"id"`
	Title          string        `json:"title"`
	Content        string        `json:"content"`
	VideoInfo      *videoInfo    `json:"videoInfo"`
	Type           int64         `json:"type"`
	ReadTotalCount int64         `json:"readTotalCount"`
	LikeCountTotal int64         `json:"likeCountTotal"`
	GmtCreate      int64         `json:"gmtCreate"`
	ReplyCount     int64         `json:"replyCount"`
	Images         []image       `json:"images"`
	PostURL        string        `json:"postUrl"`
	CommunityUser  communityUser `json:"communityUserItem"`
	UserRegion     string        `json:"userRegion"`
}

type news struct {
	NewsID        int64         `json:"newsId"`
	GameType      int64         `json:"gameType"`
	Title         string        `json:"title"`
	CommunityUser communityUser `json:"communityUserItem"`
	Content       string        `json:"content"`
	PageViewCount int64         `json:"pageViewCount"`
	PublishTime   int64         `json:"publishTime"`
	UserRegion    string        `json:"userRegion"`
	LikeCount     int64         `json:"likeCount"`
}

func (p *Parser) post(ctx context.Context, m *parse.Match) (*model.Result, error) {
	postID := strconv.FormatInt(m.Int("id"), 10)
	var got struct {
		Result struct {
			Post post `json:"post"`
		} `json:"result"`
	}
	if err := p.client.GetJSON(postAPI+postID, &got, nil); err != nil {
		return nil, err
	}

	item := got.Result.Post
	target := item.PostURL
	if target == "" {
		target = "https://news.wmpvp.com/community-detail.html?id=" + postID
	}

	rich := htmlx.Rich(item.Content)
	content := make([]model.Content, 0, len(rich.Images)+len(item.Images)+2)
	if rich.Text != "" {
		content = append(content, model.Text{Value: rich.Text})
	}
	for _, img := range item.Images {
		if img.URL != "" {
			content = append(content, model.Image{URL: img.URL})
		}
	}
	if item.VideoInfo != nil && len(item.VideoInfo.PlayInfoList) > 0 {
		content = append(content, model.Video{
			URL:      item.VideoInfo.PlayInfoList[0].PlayURL,
			Cover:    item.VideoInfo.VideoBase.CoverURL,
			Duration: duration(item.VideoInfo.VideoBase.Duration),
			Headers:  headers,
		})
	}

	return &model.Result{
		Platform:  platform,
		URL:       target,
		Title:     item.Title,
		Author:    model.Author{Name: item.CommunityUser.Nickname, ID: strconv.FormatInt(item.CommunityUser.UserID, 10), Avatar: item.CommunityUser.Avatar},
		Timestamp: item.GmtCreate / 1000,
		Content:   content,
		Stats: model.Stats{
			View:    htmlx.Num(item.ReadTotalCount),
			Like:    htmlx.Num(item.LikeCountTotal),
			Comment: htmlx.Num(item.ReplyCount),
		},
	}, nil
}

func (p *Parser) news(ctx context.Context, m *parse.Match) (*model.Result, error) {
	query := url.Values{}
	query.Set("gameType", strconv.FormatInt(m.Int("gameTypeStr"), 10))
	query.Set("newsId", strconv.FormatInt(m.Int("id"), 10))

	var got struct {
		Result struct {
			News news `json:"news"`
		} `json:"result"`
	}
	if err := p.client.GetJSON(newsAPI+"?"+query.Encode(), &got, nil); err != nil {
		return nil, err
	}

	item := got.Result.News
	rich := htmlx.Rich(item.Content)
	content := make([]model.Content, 0, len(rich.Images)+1)
	if rich.Text != "" {
		content = append(content, model.Text{Value: rich.Text})
	}
	for _, img := range rich.Images {
		content = append(content, model.Image{URL: img})
	}

	return &model.Result{
		Platform: platform,
		URL: "https://news.wmpvp.com/news.html?id=" + strconv.FormatInt(item.NewsID, 10) +
			"&gameTypeStr=" + strconv.FormatInt(item.GameType, 10),
		Title:     item.Title,
		Author:    model.Author{Name: item.CommunityUser.Nickname, ID: strconv.FormatInt(item.CommunityUser.UserID, 10), Avatar: item.CommunityUser.Avatar},
		Timestamp: item.PublishTime / 1000,
		Content:   content,
		Stats: model.Stats{
			View: htmlx.Num(item.PageViewCount),
			Like: htmlx.Num(item.LikeCount),
		},
	}, nil
}

func duration(value string) float64 {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return seconds
}

func init() { parse.Register(New) }
