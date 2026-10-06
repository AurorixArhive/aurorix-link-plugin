package transport

import (
	"os"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"video-plugin/internal/detect"
	"video-plugin/internal/model"
	"video-plugin/internal/parse"
)

// 真实网络用例: 默认跳过, VIDEO_LIVE=1 时才跑。
// 站点接口随时会变, 这些用例是"现在到底通不通"的证据, 不是回归测试。
func liveSet(t *testing.T) *parse.Set {
	t.Helper()
	if os.Getenv("VIDEO_LIVE") == "" {
		t.Skip("设置 VIDEO_LIVE=1 才跑真实网络")
	}
	return parse.New(HTTP{Client: requests.Init(20)}, nil)
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
		t.Errorf("四个平台都不该只出文案")
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

func TestLiveKuaishou(t *testing.T) {
	set := liveSet(t)
	for _, link := range []string{
		"https://v.kuaishou.com/2xZPkuV",
		"https://v.kuaishou.com/1ff8QP",
	} {
		media, err := set.Do(detect.Kuaishou, link)
		t.Logf("--- %s", link)
		report(t, media, err)
	}
}

func TestLiveXhs(t *testing.T) {
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

func TestLiveHeybox(t *testing.T) {
	set := liveSet(t)
	for _, link := range []string{
		"https://www.xiaoheihe.cn/app/bbs/link/174972336",
		"https://www.xiaoheihe.cn/app/bbs/link/181099114",
	} {
		media, err := set.Do(detect.Heybox, link)
		t.Logf("--- %s", link)
		report(t, media, err)
	}
}

// TestLiveSweep 一次跑完各平台并逐条打印结果。
// 与上面的用例不同, 失败只记录不中断 —— 目的是在一条命令里看全每个平台"现在到底通不通"。
func TestLiveSweep(t *testing.T) {
	set := liveSet(t)
	cases := []struct {
		platform string
		link     string
	}{
		{detect.Acfun, "https://www.acfun.cn/v/ac21081272"},
		{detect.Weibo, "https://m.weibo.cn/detail/5267058893525035"},
		{detect.Miyoushe, "https://www.miyoushe.com/ys/article/17414097"},
		{detect.Weishi, "https://video.weishi.qq.com/5D41bben"},
		{detect.Zuiyou, "https://share.xiaochuankeji.cn/hybrid/share/post?pid=423835942"},
		{detect.Ixigua, "https://www.ixigua.com/7270448082586698281"},
		{detect.Pipixia, "https://h5.pipix.com/s/hOiEIOgD1Po/"},
	}
	for _, c := range cases {
		media, err := set.Do(c.platform, c.link)
		if err != nil {
			t.Errorf("%-10s 失败: %v", detect.Name(c.platform), err)
			continue
		}
		t.Logf("%-10s 类型=%-6s 标题=%q 作者=%q 时长=%ds 图=%d 有视频=%t",
			detect.Name(c.platform), media.Kind(), media.Title, media.Author,
			media.Duration, len(media.Images), media.Video != nil)
	}
}
