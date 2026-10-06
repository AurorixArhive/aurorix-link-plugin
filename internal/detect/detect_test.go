package detect

import "testing"

func TestFindPlatformLinks(t *testing.T) {
	cases := []struct {
		text     string
		platform string
		url      string
	}{
		{"看看这个 https://v.douyin.com/iRNBho6/ 挺好笑的", Douyin, "https://v.douyin.com/iRNBho6/"},
		{"https://www.douyin.com/video/7321234567890123456", Douyin, "https://www.douyin.com/video/7321234567890123456"},
		{"分享 https://share.xiaochuankeji.cn/hybrid/share/post?pid=423835942", Zuiyou, "https://share.xiaochuankeji.cn/hybrid/share/post?pid=423835942"},
		{"https://www.bilibili.com/video/BV1GJ411x7h7?share_source=copy_web", Bilibili, "https://www.bilibili.com/video/BV1GJ411x7h7?share_source=copy_web"},
		{"短链 https://b23.tv/AbCdEfG", Bilibili, "https://b23.tv/AbCdEfG"},
		{"这段讲得不错 BV1GJ411x7h7", Bilibili, "https://www.bilibili.com/video/BV1GJ411x7h7"},
		{"笔记 https://www.xiaohongshu.com/explore/65f0a1b2c3d4e5f60718293a?xsec_token=ABtoken&xsec_source=pc_share", Xhs, "https://www.xiaohongshu.com/explore/65f0a1b2c3d4e5f60718293a?xsec_token=ABtoken&xsec_source=pc_share"},
		{"https://xhslink.com/o/9aBcDe", Xhs, "https://xhslink.com/o/9aBcDe"},
		{"https://m.weibo.cn/detail/5267058893525035", Weibo, "https://m.weibo.cn/detail/5267058893525035"},
	}
	for _, c := range cases {
		hit, ok := Find(c.text)
		if !ok {
			t.Fatalf("%q 未识别出链接", c.text)
		}
		if hit.Platform != c.platform || hit.URL != c.url {
			t.Fatalf("%q 识别为 %s %q, want %s %q", c.text, hit.Platform, hit.URL, c.platform, c.url)
		}
	}
}

// 链接尾巴上的中文标点必须切掉, 否则 query 与路径都会带上垃圾。
func TestFindTrimsChinesePunctuation(t *testing.T) {
	hit, ok := Find("转发一下（https://v.douyin.com/iRNBho6/），谢谢")
	if !ok {
		t.Fatal("未识别出链接")
	}
	if hit.URL != "https://v.douyin.com/iRNBho6/" {
		t.Fatalf("链接尾部未清理: %q", hit.URL)
	}
}

func TestFindIgnoresOtherContent(t *testing.T) {
	for _, text := range []string{
		"今天天气不错",
		"https://www.example.com/video/123",
		"https://live.bilibili.com/12345",
		"https://live.douyin.com/12345",
		"BV后面的数字不够长 BV123",
	} {
		if hit, ok := Find(text); ok {
			t.Fatalf("%q 不该被识别: %+v", text, hit)
		}
	}
}

func TestFindTakesFirstLink(t *testing.T) {
	text := "https://www.example.com/x 然后 https://v.douyin.com/aaa/ 最后 https://www.miyoushe.com/ys/article/17414097"
	hit, ok := Find(text)
	if !ok || hit.Platform != Douyin {
		t.Fatalf("应取第一个可解析链接: %+v", hit)
	}
}

func TestFindAllDedup(t *testing.T) {
	text := "https://v.douyin.com/aaa/ 和 https://v.douyin.com/aaa/ 还有 https://b23.tv/bbb"
	hits := FindAll(text)
	if len(hits) != 2 {
		t.Fatalf("应得到 2 条去重结果: %+v", hits)
	}
	if hits[0].Platform != Douyin || hits[1].Platform != Bilibili {
		t.Fatalf("顺序不对: %+v", hits)
	}
}

// 卡片里的链接混着封面、跳转页、小程序地址, 只能按域名挑。
func TestFromArkPicksKnownHost(t *testing.T) {
	urls := []string{
		"https://p3-sign.douyinpic.com/cover.jpg",
		"https://v.douyin.com/iRNBho6/",
		"mqqapi://aio/inlinecmd?command=x",
	}
	hit, ok := FromArk(urls)
	if !ok || hit.Platform != Douyin || hit.URL != "https://v.douyin.com/iRNBho6/" {
		t.Fatalf("卡片识别失败: %+v", hit)
	}
}

func TestFromArkWithoutKnownHost(t *testing.T) {
	if _, ok := FromArk([]string{"https://www.example.com/a", "mqqapi://x"}); ok {
		t.Fatal("没有已知域名时不该命中")
	}
}
