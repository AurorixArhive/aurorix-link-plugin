package link

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/plugin"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/send"
)

var defaults = send.Config{
	Enabled:       map[string]bool{},
	Cookies:       map[string]string{},
	GroupCooldown: 0,
	LinkTTL:       0,
	Timeout:       30 * time.Second,
	SendVideo:     true,
	MaxDuration:   10 * 60,
	MaxSizeMB:     30,
}

type settings struct {
	value   atomic.Pointer[send.Config]
	engine  *parse.Engine
	scopes  []string
	cookies map[string]parse.CookieAware
}

func newSettings(engine *parse.Engine) *settings {
	s := &settings{
		engine:  engine,
		cookies: map[string]parse.CookieAware{},
	}
	for _, parser := range parse.All() {
		if aware, ok := parser.(parse.CookieAware); ok {
			s.cookies[parser.Platform().Name] = aware
		}
	}
	for _, p := range engine.Platforms() {
		s.scopes = append(s.scopes, p.Name)
		defaults.Enabled[p.Name] = true
	}
	current := defaults
	s.value.Store(&current)
	return s
}

func (s *settings) Get() send.Config {
	if v := s.value.Load(); v != nil {
		return *v
	}
	return defaults
}

func (s *settings) Apply(values map[string]any) error {
	current := defaults
	current.Enabled = make(map[string]bool, len(s.scopes))
	for _, name := range s.scopes {
		current.Enabled[name] = true
	}
	for _, name := range split(text(values, "disabled_platforms")) {
		if id := s.platformID(name); id != "" {
			current.Enabled[id] = false
		}
	}

	current.GroupCooldown = seconds(values, "group_cooldown_seconds", defaults.GroupCooldown)
	current.LinkTTL = minutes(values, "link_ttl_minutes", defaults.LinkTTL)
	current.MaxDuration = number(values, "max_duration_minutes", defaults.MaxDuration/60) * 60
	current.MaxSizeMB = number(values, "max_size_mb", defaults.MaxSizeMB)
	current.SendVideo = boolean(values, "send_video", defaults.SendVideo)

	current.Cookies = make(map[string]string, len(s.cookies))
	for platform, aware := range s.cookies {
		value := strings.TrimSpace(text(values, "cookie_"+platform))
		if value == "" {
			continue
		}
		current.Cookies[platform] = value
		aware.SetCookie(value)
	}

	s.value.Store(&current)
	return nil
}

func (s *settings) platformID(token string) string {
	token = strings.TrimSpace(token)
	for _, p := range s.engine.Platforms() {
		if strings.EqualFold(token, p.Name) || token == p.DisplayName {
			return p.Name
		}
	}
	return ""
}

func (s *settings) configFields() []plugin.ConfigField {
	fields := []plugin.ConfigField{
		{
			Key:         "disabled_platforms",
			Label:       "关闭的平台",
			Description: "填平台名，逗号分隔，中英文名都认。留空表示全开。",
			Type:        "text",
			Placeholder: "小红书,微博",
		},
		{
			Key:         "group_cooldown_seconds",
			Label:       "群内冷却（秒）",
			Description: "同一个群/单聊两次解析之间的最小间隔。填 0 表示不限制，默认 0。",
			Type:        "number",
			Placeholder: "0",
		},
		{
			Key:         "link_ttl_minutes",
			Label:       "同一链接去重（分钟）",
			Description: "同一条链接在这段时间内只解析一次。填 0 表示每次都重新解析，默认 0。",
			Type:        "number",
			Placeholder: "0",
		},
		{
			Key:         "max_duration_minutes",
			Label:       "视频时长上限（分钟）",
			Description: "超过这个长度的视频只发卡片，不发本体。默认 10。",
			Type:        "number",
			Placeholder: "10",
		},
		{
			Key:         "max_size_mb",
			Label:       "视频体积上限（MB）",
			Description: "超过这个体积的视频只发卡片。默认 30。",
			Type:        "number",
			Placeholder: "30",
		},
		{
			Key:         "send_video",
			Label:       "发送视频本体",
			Description: "关掉后只发卡片和图片。默认开。",
			Type:        "boolean",
		},
	}

	names := make([]string, 0, len(s.cookies))
	for name := range s.cookies {
		names = append(names, name)
	}
	for _, platform := range s.platformsInOrder(names) {
		fields = append(fields, plugin.ConfigField{
			Key:         "cookie_" + platform,
			Label:       s.engine.Name(platform) + " Cookie",
			Description: "填浏览器里的完整 Cookie；留空表示匿名访问该平台。",
			Type:        "password",
			Placeholder: "浏览器 F12 → Network → 任一请求的 Cookie",
		})
	}
	return fields
}

func (s *settings) platformsInOrder(names []string) []string {
	out := make([]string, 0, len(names))
	for _, p := range s.engine.Platforms() {
		for _, name := range names {
			if name == p.Name {
				out = append(out, name)
			}
		}
	}
	return out
}

func split(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '/'
	})
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
