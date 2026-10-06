package parse

import "testing"

const heyboxTreeJSON = `{"status":"ok","result":{"link":{` +
	`"title":"某游戏测评","description":"写了一段描述","thumb":"https://img.xiaoheihe.cn/thumb.jpg",` +
	`"body":"<p>正文第一段</p><img src=\"https://img.xiaoheihe.cn/1.jpg\"><img src=\"https://img.xiaoheihe.cn/2.jpg\"><p>结尾</p>"` +
	`}}}`

func TestHeyboxLinkImagePost(t *testing.T) {
	client := newFake(map[string]string{
		"https://api.xiaoheihe.cn/bbs/app/link/tree/v2": heyboxTreeJSON,
	})
	got, err := newHeybox(testEnv(client, nil)).Parse("https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=abc123def456")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Title != "某游戏测评" || got.Cover != "https://img.xiaoheihe.cn/thumb.jpg" {
		t.Fatalf("标题或封面不对: %+v", got)
	}
	if len(got.Images) != 2 {
		t.Fatalf("正文图片数量 %d, want 2: %v", len(got.Images), got.Images)
	}
	if got.PageURL != "https://www.xiaoheihe.cn/app/bbs/link/abc123def456" {
		t.Fatalf("页面地址 %s", got.PageURL)
	}
}

func TestHeyboxLinkIDFormats(t *testing.T) {
	cases := map[string]string{
		"https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=abc123def456": "abc123def456",
		"https://www.xiaoheihe.cn/app/bbs/link/9f8e7d6c5b":                       "9f8e7d6c5b",
		"https://www.xiaoheihe.cn/community/1":                                   "",
	}
	for raw, want := range cases {
		if got := heyboxLinkID(raw); got != want {
			t.Fatalf("%s 取到 %q, want %q", raw, got, want)
		}
	}
}

// 签名结果要稳定、形状固定: 7 位, 前 5 位来自字符表, 末 2 位是十进制校验和。
func TestHeyboxSignShape(t *testing.T) {
	first := heyboxSignAt(heyboxTreePath, 1735689600, "9F8E7D6C5B4A39281706F5E4D3C2B1A0")
	second := heyboxSignAt(heyboxTreePath, 1735689600, "9F8E7D6C5B4A39281706F5E4D3C2B1A0")
	if first != second {
		t.Fatalf("同样的输入应得到同样的签名: %s / %s", first, second)
	}
	if len(first) != 7 {
		t.Fatalf("签名长度 %d, want 7 (%s)", len(first), first)
	}
	for i, r := range first[:5] {
		if !containsRune(heyboxAlphabet, r) {
			t.Fatalf("第 %d 位 %q 不在字符表里", i, r)
		}
	}
	if first[5] < '0' || first[5] > '9' || first[6] < '0' || first[6] > '9' {
		t.Fatalf("末两位应是校验数字: %s", first)
	}
}

// 换时间或换 nonce 都必须换签名, 否则服务端会当成重放。
func TestHeyboxSignDependsOnInputs(t *testing.T) {
	base := heyboxSignAt(heyboxTreePath, 1735689600, "AAAA")
	// 签名只吃时间戳的前 7 位, 所以秒级抖动改不动它 —— 这是接口算法本身的宽容度。
	// 这里用明显不同的时间, 验的是"输入变了签名就得变"。
	if base == heyboxSignAt(heyboxTreePath, 1736699600, "AAAA") {
		t.Fatal("时间变了签名应跟着变")
	}
	if base == heyboxSignAt(heyboxTreePath, 1735689600, "BBBB") {
		t.Fatal("nonce 变了签名应跟着变")
	}
	// 路径参与签名的只是前几位(交织后只取前 20 字符), 所以差异要落在靠前的路径段上
	if base == heyboxSignAt("/v3/bbs/app/api/web/share", 1735689600, "AAAA") {
		t.Fatal("路径变了签名应跟着变")
	}
}

func TestPlainTextStripsTags(t *testing.T) {
	got := plainText("<p>正文</p><img src=\"x.jpg\"><p>结尾</p>")
	if got != "正文 结尾" {
		t.Fatalf("剥标签得到 %q", got)
	}
}

func containsRune(text string, target rune) bool {
	for _, r := range text {
		if r == target {
			return true
		}
	}
	return false
}
