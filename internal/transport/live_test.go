package transport

import (
	"os"
	"strings"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"video-plugin/internal/detect"
	"video-plugin/internal/model"
	"video-plugin/internal/parse"
)

// 真实网络用例: 默认跳过, VIDEO_LIVE=1 时才跑。
// 站点接口随时会变, 这些用例是"现在到底通不通"的证据, 不是回归测试。
//
// 需要登录态的平台(小红书/微博)把 Cookie 放进 VIDEO_LIVE_COOKIE_<平台名> 环境变量,
// 例如 VIDEO_LIVE_COOKIE_XHS="web_session=..."; 没给就跳过, 不算失败。
func liveSet(t *testing.T) *parse.Set {
	t.Helper()
	if os.Getenv("VIDEO_LIVE") == "" {
		t.Skip("设置 VIDEO_LIVE=1 才跑真实网络")
	}
	return parse.New(HTTP{Client: requests.Init(20)}, func(platform string) string {
		return os.Getenv("VIDEO_LIVE_COOKIE_" + strings.ToUpper(platform))
	})
}

func requireCookie(t *testing.T, platform string) {
	t.Helper()
	if os.Getenv("VIDEO_LIVE_COOKIE_"+strings.ToUpper(platform)) == "" {
		t.Skipf("设置 VIDEO_LIVE_COOKIE_%s 才跑该平台", strings.ToUpper(platform))
	}
}

func report(t *testing.T, media model.Media, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	t.Logf("平台=%s 类型=%s 标题=%q 作者=%q 时长=%ds", media.Platform, media.Kind(), media.Title, media.Author, media.Duration)
	t.Logf("封面=%s 统计=%s 页面=%s", media.Cover, media.Stat, media.PageURL)
	if media.Video != nil {
		t.Logf("视频直链=%s", media.Video.URL)
	}
	for i, img := range media.Images {
		t.Logf("图 %d/%d %s", i+1, len(media.Images), img)
	}
	if media.Kind() == "text" {
		t.Errorf("该平台不该只出文案")
	}
}

func TestLiveBilibili(t *testing.T) {
	set := liveSet(t)
	media, err := set.Do(detect.Bilibili, "https://www.bilibili.com/video/BV1GJ411x7h7")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	t.Logf("标题=%s 作者=%s 时长=%d 封面=%s 统计=%s", media.Title, media.Author, media.Duration, media.Cover, media.Stat)
	if media.Video != nil {
		t.Logf("视频直链=%s 体积=%d", media.Video.URL, media.Video.Size)
	} else {
		t.Log("没有取到视频直链(卡片仍可用)")
	}
}

func TestLiveDouyin(t *testing.T) {
	set := liveSet(t)
	for _, link := range []string{
		"https://v.douyin.com/irvnDGfo/",
		"https://v.douyin.com/HJhdr2YooTQ/",
	} {
		media, err := set.Do(detect.Douyin, link)
		t.Logf("--- %s", link)
		report(t, media, err)
	}
}

func TestLiveMiyoushe(t *testing.T) {
	set := liveSet(t)
	media, err := set.Do(detect.Miyoushe, "https://www.miyoushe.com/ys/article/17414097")
	report(t, media, err)
}

func TestLiveZuiyou(t *testing.T) {
	set := liveSet(t)
	media, err := set.Do(detect.Zuiyou, "https://share.xiaochuankeji.cn/hybrid/share/post?pid=423835942")
	report(t, media, err)
}

func TestLiveXhs(t *testing.T) {
	requireCookie(t, detect.Xhs)
	set := liveSet(t)
	for _, link := range []string{
		"https://xhslink.com/o/7rNw5RKnE8e",
		"https://xhslink.com/m/45NsEK8minq",
	} {
		media, err := set.Do(detect.Xhs, link)
		t.Logf("--- %s", link)
		report(t, media, err)
	}
}

func TestLiveWeibo(t *testing.T) {
	requireCookie(t, detect.Weibo)
	set := liveSet(t)
	media, err := set.Do(detect.Weibo, "https://m.weibo.cn/detail/5267058893525035")
	report(t, media, err)
}
