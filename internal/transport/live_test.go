package transport

import (
	"os"
	"testing"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"video-plugin/internal/detect"
	"video-plugin/internal/parse"
)

// 真实网络用例: 默认跳过, VIDEO_LIVE=1 时才跑。
// 站点接口随时会变, 这些用例是"现在到底通不通"的证据, 不是回归测试。
func liveSet(t *testing.T) *parse.Set {
	t.Helper()
	if os.Getenv("VIDEO_LIVE") == "" {
		t.Skip("设置 VIDEO_LIVE=1 才跑真实网络")
	}
	return parse.New(HTTP{Client: requests.Init(20)})
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
