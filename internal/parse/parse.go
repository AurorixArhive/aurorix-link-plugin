package parse

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
)

type Parser interface {
	Platform() model.Platform
	Routes() []Route
}

type CookieAware interface {
	SetCookie(value string)
}

type Handler func(ctx context.Context, m *Match) (*model.Result, error)

type Route struct {
	Keyword string
	Pattern *regexp.Regexp
	Params  map[string]ParamRule
	Handle  Handler
}

type ParamRule struct {
	Optional bool
	Default  string
	Equals   string
	OneOf    []string
	AsInt    bool
}

type Match struct {
	URL    string
	Params map[string]string
}

func (m *Match) Get(key string) string { return m.Params[key] }

func (m *Match) Int(key string) int64 {
	value, _ := strconv.ParseInt(m.Params[key], 10, 64)
	return value
}

type Factory func() Parser

var registry []Factory

func Register(f Factory) { registry = append(registry, f) }

func All() []Parser {
	out := make([]Parser, 0, len(registry))
	for _, f := range registry {
		out = append(out, f())
	}
	return out
}

type Hit struct {
	Parser Parser
	Route  Route
	Match  *Match
}

type Engine struct {
	routes []boundRoute
}

type boundRoute struct {
	parser Parser
	route  Route
}

func NewEngine() *Engine {
	engine := &Engine{}
	for _, parser := range All() {
		for _, route := range parser.Routes() {
			engine.routes = append(engine.routes, boundRoute{parser: parser, route: route})
		}
	}
	sort.SliceStable(engine.routes, func(i, j int) bool {
		return len(engine.routes[i].route.Keyword) > len(engine.routes[j].route.Keyword)
	})
	return engine
}

func (e *Engine) Find(raw string) (*Hit, error) {
	for _, br := range e.routes {
		if !strings.Contains(raw, br.route.Keyword) {
			continue
		}
		match, ok := br.route.locate(raw)
		if !ok {
			continue
		}
		return &Hit{Parser: br.parser, Route: br.route, Match: match}, nil
	}
	return nil, ErrNoMatch
}

var ErrNoMatch = errors.New("没有匹配的平台")

func (r Route) locate(raw string) (*Match, bool) {
	groups := r.Pattern.FindStringSubmatch(raw)
	if groups == nil {
		return nil, false
	}
	matched := groups[0]
	parsed, err := url.Parse(matched)
	if err != nil {
		return nil, false
	}

	params := map[string]string{}
	for i, name := range r.Pattern.SubexpNames() {
		if i > 0 && i < len(groups) && name != "" && groups[i] != "" {
			params[name] = groups[i]
		}
	}
	for key, values := range parsed.Query() {
		if len(values) > 0 && params[key] == "" {
			params[key] = values[0]
		}
	}
	for key, values := range fragmentQuery(parsed) {
		if len(values) > 0 && params[key] == "" {
			params[key] = values[0]
		}
	}

	for name, rule := range r.Params {
		value, has := params[name]
		if !has && rule.Default != "" {
			value, has = rule.Default, true
			params[name] = value
		}
		if !has {
			if rule.Optional {
				continue
			}
			return nil, false
		}
		if rule.Equals != "" && value != rule.Equals {
			return nil, false
		}
		if len(rule.OneOf) > 0 && !contains(rule.OneOf, value) {
			return nil, false
		}
		if rule.AsInt {
			if _, err := strconv.Atoi(value); err != nil {
				return nil, false
			}
		}
	}
	return &Match{URL: matched, Params: params}, true
}

func fragmentQuery(u *url.URL) url.Values {
	if u.Fragment == "" {
		return nil
	}
	i := strings.IndexByte(u.Fragment, '?')
	if i < 0 {
		return nil
	}
	values, err := url.ParseQuery(u.Fragment[i+1:])
	if err != nil {
		return nil
	}
	return values
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func Compile(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

func Domain(keyword string) *regexp.Regexp {
	return Compile(`https?://(?:[A-Za-z0-9-]+\.)*` + regexp.QuoteMeta(keyword) + `[^\s]*`)
}
