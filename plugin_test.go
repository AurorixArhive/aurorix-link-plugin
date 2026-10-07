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

func TestCardTemplateRenders(t *testing.T) {
	content, err := templates.FillFor(PluginID, "card", templates.Args{
		"emoji":    "🎬",
		"platform": "B站",
		"title":    "标题",
		"url":      "https://www.bilibili.com/video/BV1GJ411x7h7",
		"meta":     "索尼音乐中国 · 03:33",
		"desc":     "一段简介",
		"note":     "",
	})
	if err != nil {
		t.Fatalf("卡片模板渲染失败: %v", err)
	}
	for _, want := range []string{"🎬 B站", "[**标题**](https://www.bilibili.com/video/BV1GJ411x7h7)", "索尼音乐中国 · 03:33", "> 一段简介"} {
		if !strings.Contains(content, want) {
			t.Fatalf("卡片缺少 %q:\n%s", want, content)
		}
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
