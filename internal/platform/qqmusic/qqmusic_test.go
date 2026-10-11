package qqmusic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/netx"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
)

const detailBody = `{"code":0,"data":[{"name":"晴天","interval":269,
	"singer":[{"name":"周杰伦"}],"album":{"mid":"000MkMni19ClKG"},
	"file":{"media_mid":"003Qui1q2u1Zho"}}]}`

func stub(t *testing.T, vkey string, resolve string) *Parser {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/detail", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("songmid"); got != "0039MnYb0qxYhV" {
			http.Error(w, "songmid 不对: "+got, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(detailBody))
	})
	mux.HandleFunc("/vkey", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(vkey))
	})
	mux.HandleFunc("/resolve", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("msg"); got != "晴天 周杰伦" {
			http.Error(w, "关键词不对: "+got, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(resolve))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	oldDetail, oldVkey, oldResolve := detailAPI, vkeyAPI, resolveAPI
	detailAPI, vkeyAPI, resolveAPI = server.URL+"/detail", server.URL+"/vkey", server.URL+"/resolve"
	t.Cleanup(func() { detailAPI, vkeyAPI, resolveAPI = oldDetail, oldVkey, oldResolve })

	return &Parser{client: netx.New(20, headers)}
}

// 免费曲官方 vkey 直接给链, 不该再去问云汐。
func TestSongUsesOfficialVkey(t *testing.T) {
	p := stub(t, `{"req_0":{"data":{"midurlinfo":[{"purl":"M800003Qui1q2u1Zho.mp3?vkey=abc"}]}}}`,
		`{"data":{"song":"不该走到这里","singer":"无","music":"http://wrong"}}`)

	got, err := p.song(context.Background(), &parse.Match{Params: map[string]string{"song_mid": "0039MnYb0qxYhV"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Title != "晴天" || got.Author.Name != "周杰伦" {
		t.Fatalf("标题或歌手不对: %+v", got)
	}
	if len(got.Images()) != 1 || !strings.Contains(got.Images()[0], "000MkMni19ClKG") {
		t.Errorf("封面不对: %v", got.Images())
	}
	audios := got.Audios()
	if len(audios) != 1 || !strings.HasPrefix(audios[0].URL, "https://ws.stream.qqmusic.qq.com/") {
		t.Fatalf("该用官方 vkey 的链: %+v", audios)
	}
}

// 会员曲官方回空 purl, 这时才轮到云汐。
func TestSongFallsBackToYunxiForPaid(t *testing.T) {
	p := stub(t, `{"req_0":{"data":{"midurlinfo":[{"purl":""}]}}}`,
		`{"data":{"song":"晴天","singer":"周杰伦","music":"http://ws.stream.qqmusic.qq.com/M800003Qui1q2u1Zho.mp3"}}`)

	got, err := p.song(context.Background(), &parse.Match{Params: map[string]string{"song_mid": "0039MnYb0qxYhV"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	audios := got.Audios()
	if len(audios) != 1 || audios[0].Duration != 269 {
		t.Fatalf("会员曲该从云汐拿到链: %+v", audios)
	}
}

// 按歌名查很容易撞翻唱, 名字对不上就只发卡片。
func TestSongDropsWrongNameFromYunxi(t *testing.T) {
	p := stub(t, `{"req_0":{"data":{"midurlinfo":[{"purl":""}]}}}`,
		`{"data":{"song":"晴天","singer":"Lucky小爱","music":"http://ws.stream.qqmusic.qq.com/x.mp3"}}`)

	got, err := p.song(context.Background(), &parse.Match{Params: map[string]string{"song_mid": "0039MnYb0qxYhV"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(got.Audios()) != 0 {
		t.Fatalf("歌手对不上时不该给音频: %+v", got.Audios())
	}
}

func TestSongNeedsMid(t *testing.T) {
	p := &Parser{client: netx.New(20, headers)}
	if _, err := p.song(context.Background(), &parse.Match{Params: map[string]string{}}); err == nil {
		t.Fatal("没有 songmid 时该报错")
	}
}

func TestSongMidIn(t *testing.T) {
	cases := map[string]string{
		"https://y.qq.com/n/ryqq/songDetail/0039MnYb0qxYhV":  "0039MnYb0qxYhV",
		"https://y.qq.com/n/yqq/song/001Qu4I30eVFYb.html":    "001Qu4I30eVFYb",
		`"songmid":"003UkWuI0E8U0l"`:                         "003UkWuI0E8U0l",
		"https://y.qq.com/n/ryqq/albumDetail/000MkMni19ClKG": "",
	}
	for in, want := range cases {
		if got := songMidIn(in); got != want {
			t.Errorf("songMidIn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSingersJoins(t *testing.T) {
	got := singers([]singer{{Name: "周杰伦"}, {Name: "费玉清"}})
	if got != "周杰伦、费玉清" {
		t.Errorf("singers() = %q", got)
	}
}
