package doubao

import (
	"context"
	"strconv"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Referer":    "https://www.doubao.com/",
}

const shareAPI = "https://www.doubao.com/creativity/share/get_video_share_info"

var platform = model.Platform{Name: "doubao", DisplayName: "豆包"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{{
		Keyword: "doubao.com/video-sharing",
		Pattern: parse.Compile(`doubao\.com/video-sharing[^\s]*`),
		Params: map[string]parse.ParamRule{
			"share_id": {},
			"video_id": {},
		},
		Handle: p.share,
	}}
}

type playInfo struct {
	Main      string `json:"main"`
	Backup    string `json:"backup"`
	PosterURL string `json:"poster_url"`
}

type userInfo struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
}

type shareResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		PlayInfo playInfo `json:"play_info"`
		UserInfo userInfo `json:"user_info"`
		Prompt   string   `json:"prompt"`
	} `json:"data"`
}

func (p *Parser) share(ctx context.Context, m *parse.Match) (*model.Result, error) {
	shareID := m.Get("share_id")
	videoID := m.Get("video_id")
	payload := map[string]any{"share_id": shareID, "vid": videoID}

	var got shareResponse
	if err := p.client.PostJSONDecode(shareAPI, payload, &got, nil); err != nil {
		return nil, err
	}
	if got.Code != 0 {
		return nil, errCode(got.Code, got.Msg)
	}

	content := make([]model.Content, 0, 2)
	if prompt := got.Data.Prompt; prompt != "" {
		content = append(content, model.Text{Value: prompt})
	}
	if main := got.Data.PlayInfo.Main; main != "" {
		content = append(content, model.Video{
			URL:     main,
			Cover:   got.Data.PlayInfo.PosterURL,
			Headers: headers,
		})
	}

	return &model.Result{
		Platform: platform,
		URL:      "https://www.doubao.com/video-sharing?share_id=" + shareID + "&video_id=" + videoID,
		Author: model.Author{
			Name: got.Data.UserInfo.Nickname,
			ID:   strconv.FormatInt(got.Data.UserInfo.UserID, 10),
		},
		Content: content,
	}, nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

func errCode(code int, msg string) error {
	if msg == "" {
		msg = "未知错误"
	}
	return errorString("豆包接口返回错误 " + strconv.Itoa(code) + ": " + msg)
}
