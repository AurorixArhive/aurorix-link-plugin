// Package transport 把框架的 HTTP 客户端接到解析内核要的形状上。
//
// 解析内核只认 parse.Client, 这一层是唯一的适配点 —— 框架的请求实现换了, 只改这里。
package transport

import (
	"net/http"

	"github.com/KasumiYuku/Aurorix/lib/requests"

	"github.com/AurorixArhive/aurorix-video-plugin/internal/parse"
)

type HTTP struct {
	Client *requests.Client
}

func (h HTTP) Get(rawURL string, headers map[string]string) (*parse.Response, error) {
	resp, err := h.Client.DoResponse(http.MethodGet, rawURL, nil, headers)
	if err != nil {
		return nil, err
	}
	return &parse.Response{
		StatusCode: resp.StatusCode,
		URL:        resp.URL,
		Header:     resp.Header,
		Body:       resp.Body,
	}, nil
}

func (h HTTP) SetCookie(rawURL, name, value string) error {
	return h.Client.SetCookie(rawURL, name, value)
}
