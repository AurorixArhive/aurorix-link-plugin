package media

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 这条判断决定"把 URL 交给平台"还是"自己下回来再传", 判错就会白等一场然后什么都没有。
func TestPubliclyFetchable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			t.Error("应该只取 1 字节, 实际没带 Range")
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("x"))
	})
	mux.HandleFunc("/hotlink", func(w http.ResponseWriter, r *http.Request) {
		// B站那种: 没带 Referer 就拒
		if r.Header.Get("Referer") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte("x"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if !PubliclyFetchable(srv.URL+"/ok", 5*time.Second) {
		t.Fatal("206 应算可拉")
	}
	if PubliclyFetchable(srv.URL+"/hotlink", 5*time.Second) {
		t.Fatal("403 不该算可拉 —— 这正是 B站直链的情况")
	}
	if PubliclyFetchable("http://127.0.0.1:1/nope", time.Second) {
		t.Fatal("连不上不该算可拉")
	}
}
