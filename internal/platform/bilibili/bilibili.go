package bilibili

import (
	"context"
	"fmt"
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
	"Referer":    "https://www.bilibili.com/",
	"Origin":     "https://www.bilibili.com",
}

const (
	viewAPI    = "https://api.bilibili.com/x/web-interface/view"
	playAPI    = "https://api.bilibili.com/x/player/playurl"
	seasonAPI  = "https://api.bilibili.com/pgc/view/web/season"
	pgcPlayAPI = "https://api.bilibili.com/pgc/player/web/playurl"
)

var platform = model.Platform{Name: "bilibili", DisplayName: "哔哩哔哩"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) SetCookie(value string) { p.client.SetCookie(value) }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "b23.tv",
			Pattern: parse.Compile(`b23\.tv/[0-9a-zA-Z._?%&+\-=/#]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "bili2233",
			Pattern: parse.Compile(`bili2233\.cn/[0-9a-zA-Z._?%&+\-=/#]+`),
			Handle:  p.shortLink,
		},
		{
			Keyword: "bilibili.com/bangumi/play",
			Pattern: parse.Compile(`bilibili\.com/bangumi/play/ep(?P<ep_id>\d+)[^\s]*`),
			Handle:  p.bangumi,
		},
		{
			Keyword: "bilibili.com/bangumi/play",
			Pattern: parse.Compile(`bilibili\.com/bangumi/play/ss(?P<season_id>\d+)[^\s]*`),
			Handle:  p.bangumi,
		},
		{
			Keyword: "bilibili.com/list/watchlater",
			Pattern: parse.Domain("bilibili.com/list/watchlater"),
			Params:  map[string]parse.ParamRule{"bvid": {}},
			Handle:  p.video,
		},
		{
			Keyword: "/BV",
			Pattern: parse.Compile(`bilibili\.com(?:/video)?/(?P<bvid>BV[0-9A-Za-z]{10})[^\s]*`),
			Params:  map[string]parse.ParamRule{"p": {Optional: true, Default: "1", AsInt: true}},
			Handle:  p.video,
		},
		{
			Keyword: "/av",
			Pattern: parse.Compile(`bilibili\.com(?:/video)?/av(?P<avid>\d{6,})[^\s]*`),
			Params:  map[string]parse.ParamRule{"p": {Optional: true, Default: "1", AsInt: true}},
			Handle:  p.video,
		},
		{
			Keyword: "BV",
			Pattern: parse.Compile(`^(?P<bvid>BV[0-9a-zA-Z]{10})(?:\s)?(?P<p>\d{1,3})?$`),
			Handle:  p.video,
		},
	}
}

type viewData struct {
	Bvid     string `json:"bvid"`
	Aid      int64  `json:"aid"`
	Cid      int64  `json:"cid"`
	Title    string `json:"title"`
	Desc     string `json:"desc"`
	Pic      string `json:"pic"`
	Duration int64  `json:"duration"`
	Pubdate  int64  `json:"pubdate"`
	Owner    struct {
		Mid  int64  `json:"mid"`
		Name string `json:"name"`
		Face string `json:"face"`
	} `json:"owner"`
	Stat struct {
		View     int64 `json:"view"`
		Danmaku  int64 `json:"danmaku"`
		Reply    int64 `json:"reply"`
		Favorite int64 `json:"favorite"`
		Coin     int64 `json:"coin"`
		Share    int64 `json:"share"`
		Like     int64 `json:"like"`
	} `json:"stat"`
	Pages []struct {
		Cid        int64  `json:"cid"`
		Page       int    `json:"page"`
		Part       string `json:"part"`
		Duration   int64  `json:"duration"`
		FirstFrame string `json:"first_frame"`
	} `json:"pages"`
}

type viewReply struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Data    viewData `json:"data"`
}

type dashStream struct {
	ID        int      `json:"id"`
	BaseURL   string   `json:"baseUrl"`
	BackupURL []string `json:"backupUrl"`
}

type playData struct {
	Quality int    `json:"quality"`
	Format  string `json:"format"`
	Durl    []struct {
		URL       string   `json:"url"`
		BackupURL []string `json:"backup_url"`
		Size      int64    `json:"size"`
		Length    int64    `json:"length"`
	} `json:"durl"`
	Dash struct {
		Duration int64        `json:"duration"`
		Video    []dashStream `json:"video"`
		Audio    []dashStream `json:"audio"`
	} `json:"dash"`
}

type playReply struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Data    playData `json:"data"`
	Result  playData `json:"result"`
}

type seasonReply struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Result  struct {
		SeasonID    int64  `json:"season_id"`
		SeasonTitle string `json:"season_title"`
		Title       string `json:"title"`
		Cover       string `json:"cover"`
		SquareCover string `json:"square_cover"`
		Evaluate    string `json:"evaluate"`
		ShareURL    string `json:"share_url"`
		Stat        struct {
			Views    int64 `json:"views"`
			Likes    int64 `json:"likes"`
			Favorite int64 `json:"favorite"`
			Share    int64 `json:"share"`
			Reply    int64 `json:"reply"`
			Danmakus int64 `json:"danmakus"`
			Coins    int64 `json:"coins"`
		} `json:"stat"`
		Episodes []struct {
			EpID      int64  `json:"ep_id"`
			Aid       int64  `json:"aid"`
			Bvid      string `json:"bvid"`
			Cid       int64  `json:"cid"`
			Cover     string `json:"cover"`
			Duration  int64  `json:"duration"`
			Title     string `json:"title"`
			LongTitle string `json:"long_title"`
		} `json:"episodes"`
	} `json:"result"`
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
		if route.Keyword == "b23.tv" || route.Keyword == "bili2233" {
			continue
		}
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
		if parsed, err := url.Parse(groups[0]); err == nil {
			for key, values := range parsed.Query() {
				if len(values) > 0 && params[key] == "" {
					params[key] = values[0]
				}
			}
		}
		return route.Handle(ctx, &parse.Match{URL: groups[0], Params: params})
	}
	return nil, errNoRoute
}

func (p *Parser) video(ctx context.Context, m *parse.Match) (*model.Result, error) {
	bvid := m.Get("bvid")
	avid := m.Int("avid")
	if bvid == "" && avid == 0 {
		return nil, errNoID
	}
	page := m.Int("p")
	if page < 1 {
		page = 1
	}
	return p.buildVideo(bvid, avid, int(page))
}

func (p *Parser) buildVideo(bvid string, avid int64, page int) (*model.Result, error) {
	view, err := p.viewInfo(bvid, avid)
	if err != nil {
		return nil, err
	}

	pageIndex := page - 1
	title := view.Title
	duration := view.Duration
	cover := view.Pic
	if len(view.Pages) > 1 {
		pageIndex %= len(view.Pages)
		part := view.Pages[pageIndex]
		if part.Part != "" {
			title = fmt.Sprintf("%s | 分集 - %s", title, part.Part)
		}
		duration = part.Duration
		if part.FirstFrame != "" {
			cover = part.FirstFrame
		}
	} else if len(view.Pages) > 0 {
		pageIndex = 0
	}
	if pageIndex < 0 || pageIndex >= len(view.Pages) {
		pageIndex = 0
	}
	cid := view.Cid
	if pageIndex < len(view.Pages) {
		cid = view.Pages[pageIndex].Cid
	}

	content := make([]model.Content, 0, 2)
	videoURL, audioURL := p.streams(playAPI, map[string]string{
		"avid":  strconv.FormatInt(view.Aid, 10),
		"bvid":  view.Bvid,
		"cid":   strconv.FormatInt(cid, 10),
		"qn":    "80",
		"fnval": "0",
		"fourk": "1",
	})
	if videoURL != "" {
		content = append(content, model.Video{
			URL:      videoURL,
			Cover:    cover,
			Duration: float64(duration),
			Headers:  headers,
		})
	}
	if audioURL != "" {
		content = append(content, model.Audio{URL: audioURL, Headers: headers})
	}
	if desc := htmlx.Compact(view.Desc); desc != "" {
		content = append(content, model.Text{Value: desc})
	}

	link := "https://www.bilibili.com/video/" + view.Bvid
	if view.Bvid == "" {
		link = "https://www.bilibili.com/video/av" + strconv.FormatInt(view.Aid, 10)
	}
	if pageIndex > 0 {
		link += fmt.Sprintf("?p=%d", pageIndex+1)
	}

	return &model.Result{
		Platform: platform,
		URL:      link,
		Title:    title,
		Author: model.Author{
			Name:   view.Owner.Name,
			ID:     strconv.FormatInt(view.Owner.Mid, 10),
			Avatar: view.Owner.Face,
		},
		Stats: model.Stats{
			View:    htmlx.Num(view.Stat.View),
			Like:    htmlx.Num(view.Stat.Like),
			Collect: htmlx.Num(view.Stat.Favorite),
			Share:   htmlx.Num(view.Stat.Share),
			Comment: htmlx.Num(view.Stat.Reply),
			Extra: [][2]string{
				{"弹幕", htmlx.Num(view.Stat.Danmaku)},
				{"硬币", htmlx.Num(view.Stat.Coin)},
			},
		},
		Timestamp: view.Pubdate,
		Content:   content,
	}, nil
}

func (p *Parser) bangumi(ctx context.Context, m *parse.Match) (*model.Result, error) {
	query := url.Values{}
	if epID := m.Get("ep_id"); epID != "" {
		query.Set("ep_id", epID)
	} else {
		query.Set("season_id", m.Get("season_id"))
	}

	var got seasonReply
	if err := p.client.GetJSON(seasonAPI+"?"+query.Encode(), &got, nil); err != nil {
		return nil, err
	}
	if got.Code != 0 {
		return nil, fmt.Errorf("bilibili 番剧返回 %d: %s", got.Code, got.Message)
	}
	info := got.Result

	title := info.Title
	if title == "" {
		title = info.SeasonTitle
	}

	content := make([]model.Content, 0, 4)
	if info.Cover != "" {
		content = append(content, model.Image{URL: info.Cover})
	}
	if evaluate := htmlx.Compact(info.Evaluate); evaluate != "" {
		content = append(content, model.Text{Value: evaluate})
	}

	episodeIndex := 0
	if epID := m.Get("ep_id"); epID != "" {
		for i, ep := range info.Episodes {
			if strconv.FormatInt(ep.EpID, 10) == epID {
				episodeIndex = i
				break
			}
		}
	}
	if len(info.Episodes) > 0 {
		ep := info.Episodes[episodeIndex]
		videoURL, audioURL := p.streams(pgcPlayAPI, map[string]string{
			"ep_id": strconv.FormatInt(ep.EpID, 10),
			"avid":  strconv.FormatInt(ep.Aid, 10),
			"cid":   strconv.FormatInt(ep.Cid, 10),
			"qn":    "80",
			"fnval": "0",
			"fourk": "1",
		})
		if videoURL != "" {
			cover := ep.Cover
			if cover == "" {
				cover = info.Cover
			}
			content = append(content, model.Video{
				URL:      videoURL,
				Cover:    cover,
				Duration: float64(ep.Duration) / 1000,
				Headers:  headers,
			})
		}
		if audioURL != "" {
			content = append(content, model.Audio{URL: audioURL, Headers: headers})
		}
	}

	link := info.ShareURL
	if link == "" {
		link = "https://www.bilibili.com/bangumi/play/ep" + m.Get("ep_id")
	}

	return &model.Result{
		Platform: platform,
		URL:      link,
		Title:    title,
		Author: model.Author{
			Name:   info.SeasonTitle,
			Avatar: info.SquareCover,
		},
		Stats: model.Stats{
			View:    htmlx.Num(info.Stat.Views),
			Like:    htmlx.Num(info.Stat.Likes),
			Collect: htmlx.Num(info.Stat.Favorite),
			Share:   htmlx.Num(info.Stat.Share),
			Comment: htmlx.Num(info.Stat.Reply),
			Extra: [][2]string{
				{"弹幕", htmlx.Num(info.Stat.Danmakus)},
				{"硬币", htmlx.Num(info.Stat.Coins)},
			},
		},
		Content: content,
	}, nil
}

func (p *Parser) viewInfo(bvid string, avid int64) (*viewData, error) {
	query := url.Values{}
	if bvid != "" {
		query.Set("bvid", bvid)
	} else {
		query.Set("aid", strconv.FormatInt(avid, 10))
	}
	var got viewReply
	if err := p.client.GetJSON(viewAPI+"?"+query.Encode(), &got, nil); err != nil {
		return nil, err
	}
	if got.Code != 0 {
		return nil, fmt.Errorf("bilibili 稿件返回 %d: %s", got.Code, got.Message)
	}
	return &got.Data, nil
}

func (p *Parser) streams(api string, query map[string]string) (string, string) {
	values := url.Values{}
	for key, value := range query {
		if value != "" {
			values.Set(key, value)
		}
	}
	var got playReply
	if err := p.client.GetJSON(api+"?"+values.Encode(), &got, nil); err != nil {
		return "", ""
	}
	if got.Code != 0 {
		return "", ""
	}
	data := got.Data
	if data.Quality == 0 && data.Format == "" {
		data = got.Result
	}
	if len(data.Durl) > 0 && data.Durl[0].URL != "" {
		return data.Durl[0].URL, ""
	}
	return bestStream(data.Dash.Video), bestStream(data.Dash.Audio)
}

func bestStream(list []dashStream) string {
	url := ""
	best := -1
	for _, item := range list {
		if item.BaseURL != "" && item.ID > best {
			best = item.ID
			url = item.BaseURL
		}
	}
	return url
}

var errNoRoute = errorString("b23 短链没有解析出可用地址")

var errNoID = errorString("缺少 bvid 或 avid")

type errorString string

func (e errorString) Error() string { return string(e) }
