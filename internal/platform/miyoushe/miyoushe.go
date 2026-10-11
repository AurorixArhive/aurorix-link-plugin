package miyoushe

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/htmlx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

var headers = map[string]string{
	"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Referer":    "https://www.miyoushe.com/",
}

const (
	postAPI = "https://bbs-api.miyoushe.com/post/wapi/getPostFull"
	ugcAPI  = "https://bbs-api.miyoushe.com/community/ugc_community/web/api/level/full/info"
)

var gameNames = map[int64]string{
	1: "bh3", 2: "ys", 3: "bh2", 4: "wd", 5: "dby", 6: "sr", 8: "zzz", 9: "hna", 10: "planet",
}

var platform = model.Platform{Name: "miyoushe", DisplayName: "米游社"}

type Parser struct{ client *netx.Client }

func New() parse.Parser { return &Parser{client: netx.New(20, headers)} }

func init() { parse.Register(New) }

func (p *Parser) Platform() model.Platform { return platform }

func (p *Parser) Routes() []parse.Route {
	return []parse.Route{
		{
			Keyword: "miyoushe.com",
			Pattern: parse.Compile(`article/(?P<post_id>\d+)`),
			Handle:  p.post,
		},
		{
			Keyword: "act.miyoushe.com/ys/ugc_community/mx",
			Pattern: parse.Domain("act.miyoushe.com/ys/ugc_community/mx"),
			Params: map[string]parse.ParamRule{
				"id":     {AsInt: true},
				"region": {},
			},
			Handle: p.ugc,
		},
	}
}

type postDetail struct {
	PostID            string   `json:"post_id"`
	GameID            int64    `json:"game_id"`
	Subject           string   `json:"subject"`
	StructuredContent string   `json:"structured_content"`
	Images            []string `json:"images"`
	CreatedAt         int64    `json:"created_at"`
	UpdatedAt         int64    `json:"updated_at"`
	ViewType          int64    `json:"view_type"`
}

type postUser struct {
	UID           string `json:"uid"`
	Nickname      string `json:"nickname"`
	Introduce     string `json:"introduce"`
	AvatarURL     string `json:"avatar_url"`
	Certification struct {
		Label string `json:"label"`
	} `json:"certification"`
}

type postStat struct {
	ViewNum     int64 `json:"view_num"`
	ReplyNum    int64 `json:"reply_num"`
	LikeNum     int64 `json:"like_num"`
	BookmarkNum int64 `json:"bookmark_num"`
	ForwardNum  int64 `json:"forward_num"`
	ShareNum    int64 `json:"share_num"`
}

type postResponse struct {
	Retcode int    `json:"retcode"`
	Message string `json:"message"`
	Data    struct {
		Post struct {
			Post postDetail `json:"post"`
			User postUser   `json:"user"`
			Stat postStat   `json:"stat"`
		} `json:"post"`
	} `json:"data"`
}

func (p *Parser) post(ctx context.Context, m *parse.Match) (*model.Result, error) {
	postID := m.Get("post_id")
	var got postResponse
	if err := p.client.GetJSON(postAPI+"?post_id="+postID, &got, nil); err != nil {
		return nil, err
	}
	if got.Retcode != 0 {
		return nil, errCode(got.Retcode, got.Message)
	}

	detail := got.Data.Post.Post
	content := structured(detail.StructuredContent)
	if detail.ViewType == 2 {
		for _, image := range detail.Images {
			if image != "" {
				content = append(content, model.Image{URL: image})
			}
		}
	}

	stat := got.Data.Post.Stat
	user := got.Data.Post.User
	desc := user.Introduce
	if desc == "" {
		desc = user.Certification.Label
	}
	game := gameNames[detail.GameID]
	if game == "" {
		game = "ys"
	}

	return &model.Result{
		Platform: platform,
		URL:      "https://m.miyoushe.com/" + game + "/#/article/" + postID,
		Title:    detail.Subject,
		Author: model.Author{
			Name:   user.Nickname,
			ID:     user.UID,
			Avatar: user.AvatarURL,
			Desc:   desc,
		},
		Stats: model.Stats{
			View:    htmlx.Num(stat.ViewNum),
			Like:    htmlx.Num(stat.LikeNum),
			Collect: htmlx.Num(stat.BookmarkNum),
			Share:   htmlx.Num(stat.ShareNum + stat.ForwardNum),
			Comment: htmlx.Num(stat.ReplyNum),
		},
		Timestamp: detail.CreatedAt,
		Content:   content,
	}, nil
}

type structuredItem struct {
	Insert json.RawMessage `json:"insert"`
}

type insertObject struct {
	Vod            *insertVideo    `json:"vod"`
	LinkCard       *insertLinkCard `json:"link_card"`
	Image          *string         `json:"image"`
	CustomEmoticon *insertEmoticon `json:"custom_emoticon"`
	BackupText     string          `json:"backup_text"`
	Video          string          `json:"video"`
}

type insertVideo struct {
	ID          string `json:"id"`
	Duration    int64  `json:"duration"`
	Cover       string `json:"cover"`
	Resolutions []struct {
		URL string `json:"url"`
	} `json:"resolutions"`
}

type insertLinkCard struct {
	Title              string `json:"title"`
	OriginURL          string `json:"origin_url"`
	Cover              string `json:"cover"`
	OriginUserAvatar   string `json:"origin_user_avatar"`
	OriginUserNickname string `json:"origin_user_nickname"`
}

type insertEmoticon struct {
	URL string `json:"url"`
}

func structured(raw string) []model.Content {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []structuredItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}

	out := make([]model.Content, 0, len(items))
	for _, item := range items {
		enc := strings.TrimSpace(string(item.Insert))
		if enc == "" || enc == "null" {
			continue
		}
		if strings.HasPrefix(enc, `"`) {
			var text string
			if err := json.Unmarshal(item.Insert, &text); err != nil {
				continue
			}
			if text = strings.TrimSpace(text); text != "" {
				out = append(out, model.Text{Value: text})
			}
			continue
		}

		var object insertObject
		if err := json.Unmarshal(item.Insert, &object); err != nil {
			continue
		}
		switch {
		case object.Vod != nil && len(object.Vod.Resolutions) > 0:
			out = append(out, model.Video{
				URL:      object.Vod.Resolutions[0].URL,
				Cover:    object.Vod.Cover,
				Duration: float64(object.Vod.Duration),
				Headers:  headers,
			})
		case object.LinkCard != nil && object.LinkCard.OriginURL != "":
			site := object.LinkCard.OriginUserNickname
			if site == "" {
				site = "米游社"
			}
			out = append(out, model.Link{
				URL:   object.LinkCard.OriginURL,
				Title: object.LinkCard.Title,
				Site:  site,
			})
		case object.Image != nil && *object.Image != "":
			out = append(out, model.Image{URL: *object.Image})
		case object.CustomEmoticon != nil && object.CustomEmoticon.URL != "":
			out = append(out, model.Image{URL: object.CustomEmoticon.URL})
		case object.Video != "":
			if bvid := bvidOf(object.Video); bvid != "" {
				out = append(out, model.Link{
					URL:   "https://www.bilibili.com/video/" + bvid,
					Title: bvid,
					Site:  "哔哩哔哩",
				})
			}
		}
	}
	return out
}

func bvidOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("bvid")
}

type ugcResponse struct {
	Data struct {
		RespMap struct {
			ReplyCard struct {
				Data struct {
					ReplyCardResponse struct {
						ReplyCount int64 `json:"reply_count"`
					} `json:"reply_card_response"`
				} `json:"data"`
				Retcode int    `json:"retcode"`
				Message string `json:"message"`
			} `json:"reply_card"`
			DeveloperInfo struct {
				Data struct {
					DeveloperNewsResponse struct {
						Developer struct {
							MysUserInfo struct {
								AID       string `json:"aid"`
								AvatarURL string `json:"avatar_url"`
								Nickname  string `json:"nickname"`
							} `json:"mys_user_info"`
						} `json:"developer"`
					} `json:"developer_news_response"`
				} `json:"data"`
				Retcode int    `json:"retcode"`
				Message string `json:"message"`
			} `json:"developer_info"`
			LevelDetail struct {
				Data struct {
					LevelDetailResponse struct {
						LevelInfo struct {
							LevelID   string `json:"level_id"`
							Region    string `json:"region"`
							LevelName string `json:"level_name"`
							Images    []struct {
								URL string `json:"url"`
							} `json:"images"`
							HotScore   string `json:"hot_score"`
							GoodRate   string `json:"good_rate"`
							PlayType   string `json:"play_type"`
							Desc       string `json:"desc"`
							LevelIntro string `json:"level_intro"`
							VideoInfo  struct {
								VideoID    string `json:"video_id"`
								VideoURL   string `json:"video_url"`
								VideoCover string `json:"video_cover"`
							} `json:"video_info"`
						} `json:"level_info"`
					} `json:"level_detail_response"`
				} `json:"data"`
				Retcode int    `json:"retcode"`
				Message string `json:"message"`
			} `json:"level_detail"`
		} `json:"resp_map"`
	} `json:"data"`
}

func (p *Parser) ugc(ctx context.Context, m *parse.Match) (*model.Result, error) {
	levelID := m.Get("id")
	region := m.Get("region")
	payload := map[string]any{
		"level_id": levelID,
		"region":   region,
		"agg_req_list": []map[string]string{
			{"api_name": "level_detail"},
			{"api_name": "reply_card"},
			{"api_name": "developer_info"},
		},
	}

	var got ugcResponse
	if err := p.client.PostJSONDecode(ugcAPI, payload, &got, nil); err != nil {
		return nil, err
	}

	resp := got.Data.RespMap
	if resp.LevelDetail.Retcode != 0 {
		return nil, errCode(resp.LevelDetail.Retcode, resp.LevelDetail.Message)
	}

	info := resp.LevelDetail.Data.LevelDetailResponse.LevelInfo
	dev := resp.DeveloperInfo.Data.DeveloperNewsResponse.Developer.MysUserInfo

	content := make([]model.Content, 0, len(info.Images)+3)
	if info.LevelIntro != "" {
		content = append(content, model.Text{Value: info.LevelIntro})
	}
	if info.Desc != "" {
		content = append(content, model.Text{Value: info.Desc})
	}
	for _, image := range info.Images {
		if image.URL != "" {
			content = append(content, model.Image{URL: image.URL})
		}
	}
	if info.VideoInfo.VideoURL != "" {
		content = append(content, model.Video{
			URL:     info.VideoInfo.VideoURL,
			Cover:   info.VideoInfo.VideoCover,
			Headers: headers,
		})
	}

	title := info.LevelName
	if info.PlayType != "" {
		title = info.LevelName + " - " + info.PlayType
	}

	result := &model.Result{
		Platform: platform,
		URL: "https://act.miyoushe.com/ys/ugc_community/mx/#/pages/level-detail/index?id=" +
			levelID + "&region=" + region,
		Title: title,
		Author: model.Author{
			Name:   dev.Nickname,
			ID:     dev.AID,
			Avatar: dev.AvatarURL,
		},
		Stats: model.Stats{
			Like:    info.GoodRate,
			Comment: htmlx.Num(resp.ReplyCard.Data.ReplyCardResponse.ReplyCount),
		},
		Content: content,
	}
	if info.HotScore != "" {
		result.Stats.Extra = [][2]string{{"热度", info.HotScore}}
	}
	return result, nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

func errCode(code int, msg string) error {
	if msg == "" {
		msg = "未知错误"
	}
	return errorString("米游社接口返回错误 " + strconv.Itoa(code) + ": " + msg)
}
