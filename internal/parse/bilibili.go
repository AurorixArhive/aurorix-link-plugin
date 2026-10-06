package parse

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"video-plugin/internal/model"
)

const (
	bilibiliViewAPI = "https://api.bilibili.com/x/web-interface/view"
	bilibiliPlayAPI = "https://api.bilibili.com/x/player/playurl"
	bilibiliSPIAPI  = "https://api.bilibili.com/x/frontend/finger/spi"
)

var (
	bvIDPattern = regexp.MustCompile(`BV[0-9A-Za-z]{10}`)
	avIDPattern = regexp.MustCompile(`av(\d+)`)
)

type bilibili struct {
	client Client
}

func newBilibili(client Client) *bilibili { return &bilibili{client: client} }

type bilibiliView struct {
	Code int    `json:"code"`
	Msg  string `json:"message"`
	Data struct {
		Bvid     string `json:"bvid"`
		Aid      int64  `json:"aid"`
		Cid      int64  `json:"cid"`
		Title    string `json:"title"`
		Desc     string `json:"desc"`
		Pic      string `json:"pic"`
		Duration int    `json:"duration"`
		Owner    struct {
			Name string `json:"name"`
		} `json:"owner"`
		Stat struct {
			View int64 `json:"view"`
			Like int64 `json:"like"`
		} `json:"stat"`
		Pages []struct {
			Cid int64 `json:"cid"`
		} `json:"pages"`
	} `json:"data"`
}

type bilibiliPlay struct {
	Code int    `json:"code"`
	Msg  string `json:"message"`
	Data struct {
		Durl []struct {
			URL  string `json:"url"`
			Size int64  `json:"size"`
		} `json:"durl"`
	} `json:"data"`
}

func (b *bilibili) Parse(rawURL string) (model.Media, error) {
	target, err := b.follow(rawURL)
	if err != nil {
		return model.Media{}, err
	}
	query := url.Values{}
	switch {
	case bvIDPattern.MatchString(target):
		query.Set("bvid", bvIDPattern.FindString(target))
	case avIDPattern.MatchString(target):
		query.Set("aid", avIDPattern.FindStringSubmatch(target)[1])
	default:
		return model.Media{}, fmt.Errorf("没从这个链接里认出视频号")
	}

	var view bilibiliView
	if err := b.get(bilibiliViewAPI+"?"+query.Encode(), &view); err != nil {
		return model.Media{}, err
	}
	if view.Code != 0 {
		return model.Media{}, fmt.Errorf("B站没给数据: %s", nonEmpty(view.Msg, "可能已删除或仅自己可见"))
	}

	data := view.Data
	cid := data.Cid
	if cid == 0 && len(data.Pages) > 0 {
		cid = data.Pages[0].Cid
	}
	media := model.Media{
		Platform: "B站",
		Title:    strings.TrimSpace(data.Title),
		Author:   data.Owner.Name,
		Desc:     firstLine(data.Desc),
		Cover:    normalizeURL(data.Pic),
		Stat:     fmt.Sprintf("播放 %s · 点赞 %s", count(data.Stat.View), count(data.Stat.Like)),
		Duration: data.Duration,
		PageURL:  "https://www.bilibili.com/video/" + data.Bvid,
	}
	if media.Title == "" {
		media.Title = "B站视频"
	}

	if cid != 0 {
		media.Video = b.playURL(data.Aid, cid)
	}
	return media, nil
}

// follow 还原短链。b23.tv 这类短链必须靠跟随跳转后的地址才知道指向哪个视频。
func (b *bilibili) follow(rawURL string) (string, error) {
	host := hostOf(rawURL)
	if !strings.Contains(host, "b23.tv") && !strings.Contains(host, "bili2233.cn") {
		return rawURL, nil
	}
	resp, err := b.client.Get(rawURL, map[string]string{"User-Agent": BrowserUA})
	if err != nil {
		return "", fmt.Errorf("短链展开失败: %w", err)
	}
	if resp.URL != "" {
		return resp.URL, nil
	}
	return rawURL, nil
}

// playURL 取渐进式 MP4 直链: fnval=1 是单文件, 不需要合流, 体积也小。
func (b *bilibili) playURL(aid, cid int64) *model.Video {
	b.seedBuvid()

	query := url.Values{
		"avid":         {strconv.FormatInt(aid, 10)},
		"cid":          {strconv.FormatInt(cid, 10)},
		"qn":           {"64"},
		"fnval":        {"1"},
		"fnver":        {"0"},
		"fourk":        {"0"},
		"platform":     {"html5"},
		"high_quality": {"1"},
	}
	var play bilibiliPlay
	headers := map[string]string{
		"User-Agent": BrowserUA,
		"Referer":    "https://www.bilibili.com",
	}
	if err := b.get(bilibiliPlayAPI+"?"+query.Encode(), &play, headers); err != nil {
		return nil
	}
	if play.Code != 0 || len(play.Data.Durl) == 0 {
		return nil
	}
	return &model.Video{
		URL:     play.Data.Durl[0].URL,
		Size:    play.Data.Durl[0].Size,
		Headers: map[string]string{"Referer": "https://www.bilibili.com"},
	}
}

// seedBuvid 取一次设备指纹并种进 cookie 罐: 没有它 playurl 会拒。
func (b *bilibili) seedBuvid() {
	var spi struct {
		Data struct {
			B3 string `json:"b_3"`
		} `json:"data"`
	}
	if err := b.get(bilibiliSPIAPI, &spi); err != nil || spi.Data.B3 == "" {
		return
	}
	_ = b.client.SetCookie("https://www.bilibili.com", "buvid3", spi.Data.B3)
}

func (b *bilibili) get(target string, out any, headers ...map[string]string) error {
	head := map[string]string{"User-Agent": BrowserUA}
	for _, extra := range headers {
		for k, v := range extra {
			head[k] = v
		}
	}
	resp, err := b.client.Get(target, head)
	if err != nil {
		return err
	}
	return resp.JSON(out)
}
