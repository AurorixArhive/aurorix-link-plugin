package coolapk

import (
	"context"
	"regexp"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
}

const feedAPI = "https://www.coolapk1s.com/feed/"

var nextData = regexp.MustCompile(`(?s)<script[^>]*id=["']__NEXT_DATA__["'][^>]*>\s*(.*?)\s*</script>`)

var platform = model.Platform{Name: "coolapk", DisplayName: "酷安"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "coolapk1s.com/feed/",
			Pattern: parse.Compile(`coolapk1s\.com/feed/(?P<feed_id>\d+)`),
			Handle:  p.feed,
		},
		{
			Keyword: "www.coolapk.com/feed/",
			Pattern: parse.Compile(`www\.coolapk\.com/feed/(?P<feed_id>\d+)`),
			Handle:  p.feed,
		},
		{
			Keyword: "coolapk.com/feed/",
			Pattern: parse.Compile(`coolapk\.com/feed/(?P<feed_id>\d+)`),
			Handle:  p.feed,
		},
	}
}

type feedData struct {
	Title      string   `json:"title"`
	Username   string   `json:"username"`
	UserAvatar string   `json:"userAvatar"`
	Dateline   *int64   `json:"dateline"`
	Message    string   `json:"message"`
	PicArr     []string `json:"picArr"`
}

type pageProps struct {
	Feed      feedData `json:"feed"`
	ID        string   `json:"id"`
	AISummary string   `json:"aiSummary"`
}

type nextDataBody struct {
	Props struct {
		PageProps pageProps `json:"pageProps"`
	} `json:"props"`
}

func (p *Parser) feed(ctx context.Context, m *parse.Match) (*model.Result, error) {
	feedID := m.Get("feed_id")
	raw, err := p.client.Get(feedAPI+feedID, nil)
	if err != nil {
		return nil, err
	}
	found := nextData.FindSubmatch(raw)
	if found == nil {
		return nil, errNoData
	}
	var next nextDataBody
	if err := netx.Decode(found[1], &next); err != nil {
		return nil, err
	}
	feed := next.Props.PageProps.Feed

	content := make([]model.Content, 0, len(feed.PicArr)+1)
	if text := htmlx.Rich(feed.Message).Text; text != "" {
		content = append(content, model.Text{Value: text})
	}
	for _, pic := range feed.PicArr {
		if pic != "" {
			content = append(content, model.Image{URL: pic})
		}
	}

	result := &model.Result{
		Platform: platform,
		URL:      "https://www.coolapk.com/feed/" + feedID,
		Author:   model.Author{Name: feed.Username, Avatar: feed.UserAvatar},
		Content:  content,
	}
	if feed.Dateline != nil {
		result.Timestamp = *feed.Dateline
	}
	return result, nil
}

var errNoData = errorString("酷安页面没有 __NEXT_DATA__")

type errorString string

func (e errorString) Error() string { return string(e) }
