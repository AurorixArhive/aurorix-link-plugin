package parse

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"time"

	"github.com/AurorixArhive/aurorix-video-plugin/internal/model"
)

var miyousheIDPattern = regexp.MustCompile(`/(?:article|post)/(\d+)`)

// 米游社 web 端的接口版本与加盐, 版本号要和 x-rpc-app_version 头一致, 否则一律被拒。
const (
	miyousheAPI     = "https://bbs-api.miyoushe.com/post/wapi/getPostFull?post_id="
	miyousheVersion = "2.87.0"
	miyousheSalt    = "ZSHlXeQUBis52qD1kEgKt5lUYed4b7Bb"
)

type miyoushe struct {
	env *env
}

func newMiyoushe(env *env) *miyoushe { return &miyoushe{env: env} }

func (m *miyoushe) Parse(rawURL string) (model.Media, error) {
	id := miyousheID(rawURL)
	if id == "" {
		return model.Media{}, errors.New("没从这个链接里认出米游社作品号")
	}
	resp, err := m.env.Get(miyousheAPI+id, m.headers())
	if err != nil {
		return model.Media{}, fmt.Errorf("米游社接口打不开: %w", err)
	}
	var body struct {
		Message string `json:"message"`
		Data    struct {
			Post struct {
				Post map[string]any `json:"post"`
				// 视频挂在外层, 不在 post 里
				VodList []struct {
					Resolutions []struct {
						URL    string `json:"url"`
						Width  int    `json:"width"`
						Height int    `json:"height"`
					} `json:"resolutions"`
				} `json:"vod_list"`
			} `json:"post"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return model.Media{}, errors.New("米游社返回的数据看不懂")
	}
	post := body.Data.Post.Post
	if post == nil {
		return model.Media{}, fmt.Errorf("米游社没给正文: %s", nonEmpty(body.Message, "可能已删除或需要重新复制链接"))
	}
	return miyousheMedia(post, body.Data.Post.VodList, id), nil
}

func (m *miyoushe) headers() map[string]string {
	return map[string]string{
		"User-Agent":        MobileUA,
		"Referer":           "https://www.miyoushe.com/",
		"x-rpc-app_version": miyousheVersion,
		"x-rpc-client_type": "4",
		"x-rpc-language":    "zh-cn",
		"Accept-Language":   "zh-cn",
		"DS":                miyousheDS(),
	}
}

// miyousheDS 就是 sha1 之前的 md5 校验串: salt 与时间戳、随机串按固定顺序拼好再摘要。
// 随机串按官方实现取 6 位大小写字母与数字。
func miyousheDS() string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	ts := time.Now().Unix()
	nonce := make([]byte, 6)
	for i := range nonce {
		nonce[i] = letters[rand.IntN(len(letters))]
	}
	raw := "salt=" + miyousheSalt + "&t=" + strconv.FormatInt(ts, 10) + "&r=" + string(nonce)
	sum := md5.Sum([]byte(raw))
	return strconv.FormatInt(ts, 10) + "," + string(nonce) + "," + hex.EncodeToString(sum[:])
}

func miyousheID(rawURL string) string {
	if m := miyousheIDPattern.FindStringSubmatch(rawURL); m != nil {
		return m[1]
	}
	return ""
}

func miyousheMedia(post map[string]any, vodList []struct {
	Resolutions []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"resolutions"`
}, id string) model.Media {
	media := model.Media{
		Platform: "米游社",
		Title:    firstLine(nonEmpty(str(post["subject"]), miyousheDescribe(post["content"]))),
		Author:   firstString(dict(post["user"]), "nickname", "username"),
		Desc:     firstLine(miyousheDescribe(post["content"])),
		Cover:    firstHTTP(post["cover"], post["image"]),
		PageURL:  "https://www.miyoushe.com/ys/article/" + id,
	}
	if media.Title == "" {
		media.Title = "米游社帖子"
	}
	for _, item := range list(post["images"]) {
		if url := firstHTTP(item); url != "" {
			media.Images = append(media.Images, url)
		}
	}

	// 每档分辨率各给一个地址, 取第一个能用的
	for _, vod := range vodList {
		for _, res := range vod.Resolutions {
			if res.URL != "" {
				media.Video = &model.Video{
					URL:     res.URL,
					Headers: map[string]string{"User-Agent": MobileUA, "Referer": "https://www.miyoushe.com/"},
				}
				return media
			}
		}
	}
	return media
}

// miyousheDescribe 正文是一段被塞进字符串的 JSON, 解不出来就当富文本直接去标签。
func miyousheDescribe(content any) string {
	raw := str(content)
	if raw == "" {
		return ""
	}
	var node struct {
		Describe string `json:"describe"`
	}
	if json.Unmarshal([]byte(raw), &node) == nil && node.Describe != "" {
		return node.Describe
	}
	return plainText(raw)
}
