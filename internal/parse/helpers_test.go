package parse

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// fakeClient 让解析层在测试里不碰网络: 按地址喂固定响应。
type fakeClient struct {
	pages   map[string]string
	links   map[string]string
	cookies map[string]string
}

// testEnv 把假客户端包成解析器要的 env; cookies 里放登录态, 默认没有。
func testEnv(client Client, cookies map[string]string) *env {
	return &env{Client: client, creds: func(platform string) string { return cookies[platform] }}
}

func newFake(pages map[string]string) *fakeClient {
	return &fakeClient{pages: pages, links: map[string]string{}, cookies: map[string]string{}}
}

// withRedirect 登记一条短链: 请求它时按登记的目标地址返回。
func (f *fakeClient) withRedirect(from, to string) *fakeClient {
	f.links[from] = to
	return f
}

func (f *fakeClient) Get(rawURL string, _ map[string]string) (*Response, error) {
	if to, ok := f.links[rawURL]; ok {
		body := f.pages[to]
		return &Response{StatusCode: http.StatusOK, URL: to, Body: []byte(body)}, nil
	}
	if body, ok := f.pages[rawURL]; ok {
		return &Response{StatusCode: http.StatusOK, URL: rawURL, Body: []byte(body)}, nil
	}
	// 带查询串的接口按前缀登记(参数里有时间戳与签名, 没法逐个写死)
	keys := make([]string, 0, len(f.pages))
	for key := range f.pages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.HasPrefix(rawURL, key) {
			return &Response{StatusCode: http.StatusOK, URL: rawURL, Body: []byte(f.pages[key])}, nil
		}
	}
	return nil, fmt.Errorf("测试没有为 %s 准备响应", rawURL)
}

func (f *fakeClient) SetCookie(rawURL, name, value string) error {
	f.cookies[rawURL+"/"+name] = value
	return nil
}
