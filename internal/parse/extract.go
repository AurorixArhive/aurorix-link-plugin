package parse

import (
	"regexp"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
)

var (
	urlPattern = regexp.MustCompile(`https?://[^\s'"<>()（）【】]+`)
	bvPattern  = regexp.MustCompile(`\bBV[0-9A-Za-z]{10}\b`)
	trailing   = "。，、；：？！）】》」』”’,.;:!?\""
)

const bareBV = "https://www.bilibili.com/video/"

func Extract(text string) []string {
	found := urlPattern.FindAllString(text, -1)
	if len(found) == 0 {
		return nil
	}
	out := make([]string, 0, len(found))
	for _, raw := range found {
		if cleaned := strings.TrimRight(strings.TrimSpace(raw), trailing); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func (e *Engine) Match(text string) (*Hit, bool) {
	for _, raw := range Extract(text) {
		if hit, err := e.Find(raw); err == nil {
			return hit, true
		}
	}
	if bv := bvPattern.FindString(text); bv != "" {
		if hit, err := e.Find(bareBV + bv); err == nil {
			return hit, true
		}
	}
	return nil, false
}

func (e *Engine) FromArk(urls []string) (*Hit, bool) {
	for _, raw := range urls {
		if hit, err := e.Find(strings.TrimRight(strings.TrimSpace(raw), trailing)); err == nil {
			return hit, true
		}
	}
	return nil, false
}

func (e *Engine) Platforms() []model.Platform {
	seen := make(map[string]bool, len(e.routes))
	out := make([]model.Platform, 0, len(e.routes))
	for _, br := range e.routes {
		p := br.parser.Platform()
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out
}

func (e *Engine) Name(platform string) string {
	for _, p := range e.Platforms() {
		if p.Name == platform {
			return p.DisplayName
		}
	}
	return platform
}
