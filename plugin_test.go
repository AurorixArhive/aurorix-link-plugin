package video

import (
	"strings"
	"testing"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/plugin"
	"github.com/KasumiYuku/Aurorix/lib/templates"
)

// 插件注册与文案渲染是最容易"编译过了但线上是哑的"的两处, 各盯一条。
func TestPluginIsRegistered(t *testing.T) {
	for _, p := range plugin.ManagedPlugins() {
		if p.ID != PluginID {
			continue
		}
		if p.Name == "" || len(p.Fields) == 0 {
			t.Fatalf("插件信息不完整: %+v", p)
		}
		return
	}
	t.Fatalf("插件 %s 没注册上", PluginID)
}

// 卡片是用户唯一看得见的东西, 所以这里钉死整段排版, 而不只是"含有关键词":
// 有内容的段落之间恰好一个空行, 没内容的段落一个字都不留。
func TestCardTemplateRenders(t *testing.T) {
	para := func(text string) []any { return []any{map[string]any{"text": text}} }
	content, err := templates.FillFor(PluginID, "card", templates.Args{
		"emoji":    "🎬",
		"platform": "B站",
		"title":    "标题",
		"url":      "https://www.bilibili.com/video/BV1GJ411x7h7",
		"author":   para("索尼音乐中国"),
		"meta":     para("03:33 · 播放 1234.5万"),
		"desc":     para("一段简介"),
		"note":     []any{},
	})
	if err != nil {
		t.Fatalf("卡片模板渲染失败: %v", err)
	}
	want := "# 🎬 B站\n\n[**标题**](https://www.bilibili.com/video/BV1GJ411x7h7) · 索尼音乐中国\n\n03:33 · 播放 1234.5万\n\n> 一段简介\n\n"
	if content != want {
		t.Fatalf("卡片排版变了:\n得到 %q\n期望 %q", content, want)
	}
}

// 没有简介时不能留一块空引用, 有提示时提示必须自成一段 ——
// 它原先紧跟在引用块后面, 渲染出来会被并进那块引用里。
// 作者为空时标题行也不该剩下一个孤零零的分隔点。
func TestCardOmitsMissingParagraphs(t *testing.T) {
	content, err := templates.FillFor(PluginID, "card", templates.Args{
		"emoji":    "🎬",
		"platform": "抖音",
		"title":    "标题",
		"url":      "https://v.douyin.com/x/",
		"author":   []any{},
		"meta":     []any{},
		"desc":     []any{},
		"note":     []any{map[string]any{"text": "视频 42.1 MB，超过 30 MB 上限"}},
	})
	if err != nil {
		t.Fatalf("卡片模板渲染失败: %v", err)
	}
	want := "# 🎬 抖音\n\n[**标题**](https://v.douyin.com/x/)\n\n视频 42.1 MB，超过 30 MB 上限\n\n"
	if content != want {
		t.Fatalf("卡片排版变了:\n得到 %q\n期望 %q", content, want)
	}
}

// 被"认出来了却没做"时补的那句话, 渲染不出来就等于静默, 所以单盯一条。
// 解析失败不再有文案 —— 那种情况一律静默, 只留日志。
func TestNoteTemplateRenders(t *testing.T) {
	content, err := templates.FillFor(PluginID, "note", templates.Args{"reason": "刚处理过一条，稍等一下再发"})
	if err != nil {
		t.Fatalf("说明模板渲染失败: %v", err)
	}
	if !strings.Contains(content, "刚处理过一条") {
		t.Fatalf("说明文案没带出原因:\n%s", content)
	}
}

func TestDefaultsHaveEveryPlatform(t *testing.T) {
	got := newSettings().Get()
	for _, platform := range []string{"bilibili", "douyin", "xhs", "weibo", "miyoushe", "zuiyou"} {
		if !got.Enabled[platform] {
			t.Fatalf("默认应开启 %s", platform)
		}
	}
	// 冷却与去重默认关闭: 同一个链接再发一次就再解析一次, 不做任何拦截。
	if got.GroupCooldown != 0 || got.LinkTTL != 0 {
		t.Fatalf("冷却与去重默认应关闭: %+v", got)
	}
	if !got.SendVideo || got.MaxDuration != 600 || got.MaxSizeMB != 30 {
		t.Fatalf("默认值不对: %+v", got)
	}
}

func TestApplyTakesEffect(t *testing.T) {
	s := newSettings()
	if err := s.Apply(map[string]any{
		"group_cooldown_seconds": float64(30),
		"link_ttl_minutes":       "5",
		"max_duration_minutes":   float64(3),
		"max_size_mb":            float64(20),
		"send_video":             false,
		"disabled_platforms":     "微博,最右",
	}); err != nil {
		t.Fatalf("应用配置失败: %v", err)
	}

	got := s.Get()
	if got.GroupCooldown != 30*time.Second || got.LinkTTL != 5*time.Minute {
		t.Fatalf("时间类配置没生效: %+v", got)
	}
	if got.MaxDuration != 180 || got.MaxSizeMB != 20 || got.SendVideo {
		t.Fatalf("数值与开关没生效: %+v", got)
	}
	if got.Enabled["weibo"] || got.Enabled["zuiyou"] {
		t.Fatalf("关掉的平台仍开着: %+v", got.Enabled)
	}
	if !got.Enabled["douyin"] || !got.Enabled["xhs"] {
		t.Fatalf("没提到的平台不该被关掉: %+v", got.Enabled)
	}
}

// 填错的平台名不该顺手把平台关掉 —— 一个错别字全关是最难查的那种故障。
func TestApplyIgnoresUnknownPlatform(t *testing.T) {
	s := newSettings()
	if err := s.Apply(map[string]any{"disabled_platforms": "抖音, 微博啊"}); err != nil {
		t.Fatalf("应用配置失败: %v", err)
	}
	got := s.Get()
	if got.Enabled["douyin"] {
		t.Fatal("写了平台名就该关掉")
	}
	if !got.Enabled["weibo"] {
		t.Fatal("错别字不该把微博也关掉")
	}
}
