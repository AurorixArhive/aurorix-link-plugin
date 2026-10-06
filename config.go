package video

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/plugin"

	"video-plugin/internal/detect"
	"video-plugin/internal/send"
)

// defaults 是没配置时用的值。每一项都能在管理台改, 改完立刻生效。
var defaults = send.Config{
	Enabled:       allPlatforms(true),
	Cookies:       map[string]string{},
	GroupCooldown: 10 * time.Second,
	LinkTTL:       10 * time.Minute,
	Timeout:       30 * time.Second,
	SendVideo:     true,
	MaxDuration:   10 * 60,
	MaxSizeMB:     50,
}

type settings struct {
	value atomic.Pointer[send.Config]
}

func newSettings() *settings {
	s := &settings{}
	current := defaults
	s.value.Store(&current)
	return s
}

// Get 读配置快照。每读一次拿一份新值, 改配置不影响正在跑的解析。
func (s *settings) Get() send.Config {
	if v := s.value.Load(); v != nil {
		return *v
	}
	return defaults
}

func (s *settings) Apply(values map[string]any) error {
	current := defaults
	current.Enabled = allPlatforms(true)
	for _, name := range strings.FieldsFunc(text(values, "disabled_platforms"), func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '/'
	}) {
		if id := platformID(name); id != "" {
			current.Enabled[id] = false
		}
	}
	current.GroupCooldown = seconds(values, "group_cooldown_seconds", defaults.GroupCooldown)
	current.LinkTTL = minutes(values, "link_ttl_minutes", defaults.LinkTTL)
	current.MaxDuration = number(values, "max_duration_minutes", defaults.MaxDuration/60) * 60
	current.MaxSizeMB = number(values, "max_size_mb", defaults.MaxSizeMB)
	current.SendVideo = boolean(values, "send_video", defaults.SendVideo)
	current.Cookies = cookies(values)

	s.value.Store(&current)
	return nil
}

// cookies 只收填了内容的平台; 空串是"没配", 不能当成空 Cookie 发出去。
func cookies(values map[string]any) map[string]string {
	out := make(map[string]string, len(cookiePlatforms))
	for _, platform := range cookiePlatforms {
		if value := strings.TrimSpace(text(values, cookieKey(platform))); value != "" {
			out[platform] = value
		}
	}
	return out
}

func allPlatforms(enabled bool) map[string]bool {
	out := make(map[string]bool, len(detect.Platforms()))
	for _, p := range detect.Platforms() {
		out[p] = enabled
	}
	return out
}

// platformID 认英文名也认中文名, 填错的名字直接忽略(不因为一个错别字把所有平台关掉)。
func platformID(token string) string {
	token = strings.TrimSpace(token)
	for _, p := range detect.Platforms() {
		if strings.EqualFold(token, p) || token == detect.Name(p) {
			return p
		}
	}
	return ""
}

func text(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	s, _ := raw.(string)
	return s
}

func number(values map[string]any, key string, fallback int) int {
	raw, ok := values[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			if n, err := strconv.Atoi(trimmed); err == nil {
				return n
			}
		}
	}
	return fallback
}

func seconds(values map[string]any, key string, fallback time.Duration) time.Duration {
	value := number(values, key, int(fallback/time.Second))
	if value < 0 {
		return 0
	}
	return time.Duration(value) * time.Second
}

// minutes 与 seconds 分开: 配置项的单位写在不同后缀里, 用错单位就是"填 5 变成 5 秒"。
func minutes(values map[string]any, key string, fallback time.Duration) time.Duration {
	value := number(values, key, int(fallback/time.Minute))
	if value < 0 {
		return 0
	}
	return time.Duration(value) * time.Minute
}

func boolean(values map[string]any, key string, fallback bool) bool {
	raw, ok := values[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "on", "yes", "是", "开":
			return true
		case "false", "0", "off", "no", "否", "关":
			return false
		}
	}
	return fallback
}

func configFields() []plugin.ConfigField {
	fields := []plugin.ConfigField{
		{
			Key:         "group_cooldown_seconds",
			Label:       "群内冷却（秒）",
			Description: "同一个群/单聊两次解析之间的最小间隔，防止有人连着甩一堆链接把机器人刷爆。默认 10。",
			Type:        "number",
			Placeholder: "10",
		},
		{
			Key:         "link_ttl_minutes",
			Label:       "同一链接去重（分钟）",
			Description: "同一条链接在这段时间内只解析一次，别人再发同样的链接不会重复刷屏。默认 10。",
			Type:        "number",
			Placeholder: "10",
		},
		{
			Key:         "max_duration_minutes",
			Label:       "视频时长上限（分钟）",
			Description: "超过这个长度的视频只发卡片和标题，不发视频本体。默认 10。",
			Type:        "number",
			Placeholder: "10",
		},
		{
			Key:         "max_size_mb",
			Label:       "视频体积上限（MB）",
			Description: "超过这个体积的视频不发本体，只发卡片。QQ 上传大文件容易超时，默认 50。",
			Type:        "number",
			Placeholder: "50",
		},
		{
			Key:         "send_video",
			Label:       "发送视频本体",
			Description: "关掉后只发卡片和标题，省流量也省时间。默认开。",
			Type:        "boolean",
		},
		{
			Key:         "disabled_platforms",
			Label:       "关闭的平台",
			Description: "填平台名，逗号分隔，例如：小红书,微博。留空表示六个平台全开。",
			Type:        "text",
			Placeholder: "小红书,微博",
		},
	}
	return append(fields, cookieFields()...)
}

// cookiePlatforms 是要登录态才拿得到数据的平台, 每个在管理台里给一个输入框。
// 没填不影响其他平台: 该平台解析失败会退成卡片, 不会连带把别的平台搞坏。
var cookiePlatforms = []string{detect.Xhs, detect.Weibo}

func cookieKey(platform string) string { return "cookie_" + platform }

func cookieFields() []plugin.ConfigField {
	fields := make([]plugin.ConfigField, 0, len(cookiePlatforms))
	for _, platform := range cookiePlatforms {
		fields = append(fields, plugin.ConfigField{
			Key:         cookieKey(platform),
			Label:       detect.Name(platform) + " Cookie",
			Description: "填你自己浏览器里的 Cookie（" + detect.Name(platform) + " 需要登录态才能拿到作品数据）。留空表示不解析该平台。",
			Type:        "password",
			Placeholder: "浏览器 F12 → Network → 任一请求的 Cookie",
		})
	}
	return fields
}
