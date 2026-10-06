// Package parse 是解析内核: 输入一个分享链接, 输出统一的 model.Media。
//
// 这里不 import 框架 —— 内核只依赖下面这个 Client 接口, 换框架、写测试都不用动它。
package parse

import (
	"encoding/json"
	"errors"
	"net/http"

	"video-plugin/internal/detect"
	"video-plugin/internal/model"
)

var ErrUnsupported = errors.New("这个平台的链接还没支持")

// Client 解析需要的全部外部能力: 发 GET 请求、预置 cookie。
type Client interface {
	Get(rawURL string, headers map[string]string) (*Response, error)
	SetCookie(rawURL, name, value string) error
}

// Response 内核看到的响应: 状态码、实际地址(跟随跳转之后)、响应头、正文。
type Response struct {
	StatusCode int
	URL        string
	Header     http.Header
	Body       []byte
}

func (r *Response) Text() string { return string(r.Body) }

func (r *Response) JSON(v any) error {
	if len(r.Body) == 0 {
		return errors.New("响应为空")
	}
	return json.Unmarshal(r.Body, v)
}

// Parser 一个平台一个实现, 签名统一。
type Parser interface {
	Parse(rawURL string) (model.Media, error)
}

// Set 平台名 → 解析器。
type Set struct {
	parsers map[string]Parser
}

func New(client Client) *Set {
	return &Set{parsers: map[string]Parser{
		detect.Bilibili: newBilibili(client),
		detect.Douyin:   newDouyin(client),
		detect.Kuaishou: newKuaishou(client),
		detect.Xhs:      newXhs(client),
		detect.Heybox:   newHeybox(client),
	}}
}

// Do 按平台解析。
func (s *Set) Do(platform, rawURL string) (model.Media, error) {
	p, ok := s.parsers[platform]
	if !ok {
		return model.Media{}, ErrUnsupported
	}
	return p.Parse(rawURL)
}
