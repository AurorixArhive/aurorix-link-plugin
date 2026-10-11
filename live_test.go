package link

import (
	"testing"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
	_ "github.com/AurorixArhive/aurorix-link-plugin/internal/platform"
)

// 联网用例: 默认跑, 加 -short 跳过。
// 上游改字段、加风控、换域名, 单测全绿也看不出来, 只有真拉一次才知道。
func TestLivePlatforms(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试")
	}
	engine := parse.NewEngine()
	t.Logf("已注册平台 %d 个", len(engine.Platforms()))

	cases := []struct {
		name string
		url  string
	}{
		{"豆瓣", "https://m.douban.com/group/topic/490753319"},
		{"堆糖图集", "https://www.duitang.com/atlas?id=12200201"},
		{"堆糖博客", "https://www.duitang.com/blog?id=154551388"},
		{"快手", "https://v.kuaishou.com/2yAnzeZ"},
		{"抖音", "https://v.douyin.com/_2ljF4AmKL8"},
		{"Lofter", "https://miwu8436.lofter.com/post/1f1c869a_1cd26ea05"},
		{"网易云音乐", "https://music.163.com/#/song?id=1303026620"},
		{"酷狗音乐", "https://t1.kugou.com/x?hash=28F83BBD8CAB043895039E98366AD2B4"},
		{"酷我音乐", "https://www.kuwo.cn/play_detail/4406153"},
		{"B站", "https://www.bilibili.com/video/BV1GJ411x7h7"},
		{"微博文章", "https://weibo.com/ttarticle/p/show?id=2309404962180771742222"},
		{"虎扑", "https://m.hupu.com/bbs/639669147.html"},
		{"贴吧", "https://tieba.baidu.com/p/11047558695"},
		{"米游社", "https://m.miyoushe.com/zzz/#/article/76178399"},
		{"酷安", "https://www.coolapk.com/feed/30000000"},
		{"TapTap", "https://www.taptap.cn/moment/852671483971898552"},
		{"LinuxDo", "https://linux.do/t/topic/1"},
		{"知乎", "https://zhuanlan.zhihu.com/p/26064022"},
		{"DS", "https://ds.163.com/feed/62f7716fc3e1e50001d280da"},
		{"ZLB", "https://bb.zlb.ink/t/topic/48589"},
		{"完美世界电竞", "https://news.wmpvp.com/community-detail.html?id=375290247"},
		{"BUFF资讯", "https://buff.163.com/s/news-detail_share.html?article_id=87855&comment_type=211"},
		{"BUFF视频", "https://buff.163.com/s/news-detail_share.html?article_id=87832&comment_type=228"},
		{"BUFF饰品", "https://buff.163.com/s/preview_share.html?game=csgo&preview_id=V1092280822&comment_type=216"},
		{"BUFF话题", "https://buff.163.com/s/topic-detail_share.html?social_topic_post_id=P1093043595&comment_type=239"},
		{"illu文章", "https://www.illund.com/share.html?articleId%3Dfcfb0ffe76"},
		{"5EPlay", "https://csgo.5eplay.com/forum/share/1050838"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hit, ok := engine.Match(c.url)
			if !ok {
				t.Fatalf("没匹配上 %s", c.url)
			}
			got, err := hit.Route.Handle(nil, hit.Match)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			t.Logf("[%s] %q 作者=%q 图=%d 视频=%d 音频=%d 文本=%d",
				got.Platform.DisplayName, got.Title, got.Author.Name,
				len(got.Images()), len(got.Videos()), len(got.Audios()), len(got.Texts()))
			if len(got.Content) == 0 {
				t.Errorf("解析出来是空的")
			}
		})
	}
}
