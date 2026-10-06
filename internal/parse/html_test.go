package parse

import "testing"

func TestJSONAfterMatchesBraces(t *testing.T) {
	html := `<script>window.X = {"a":{"b":"} 这个括号在字符串里"},"c":[1,2,{"d":"\\\""}]};other()</script>`
	raw, ok := jsonAfter(html, "window.X")
	if !ok {
		t.Fatal("应当抓到 JSON")
	}
	want := `{"a":{"b":"} 这个括号在字符串里"},"c":[1,2,{"d":"\\\""}]}`
	if string(raw) != want {
		t.Fatalf("抓到:\n%s\nwant:\n%s", raw, want)
	}
}

func TestJSONAfterMissingMarker(t *testing.T) {
	if _, ok := jsonAfter(`<script>window.Y = {}</script>`, "window.X"); ok {
		t.Fatal("没有这个标记时不该返回内容")
	}
	if _, ok := jsonAfter(`<script>window.X = null</script>`, "window.X"); ok {
		t.Fatal("标记后面没有对象时不该返回内容")
	}
}

// 命中对象必须只跟内容有关, 不随 map 遍历顺序变化 —— 否则同一个页面会时而解析出 A 时而 B。
func TestFindMapIsDeterministic(t *testing.T) {
	root := map[string]any{
		"b": map[string]any{"desc": "b 层", "video": map[string]any{}},
		"a": map[string]any{"desc": "a 层", "video": map[string]any{}},
	}
	first := str(findMap(root, []string{"desc"}, []string{"video"})["desc"])
	for i := 0; i < 20; i++ {
		if got := str(findMap(root, []string{"desc"}, []string{"video"})["desc"]); got != first {
			t.Fatalf("第 %d 次得到 %q, 首次是 %q", i, got, first)
		}
	}
	if first != "a 层" {
		t.Fatalf("按 key 排序应命中 a 层, 得到 %q", first)
	}
}

// 数组里按顺序取: 作品列表的第一项才是当前这条, 后面的常是推荐位。
func TestFindMapTakesFirstArrayItem(t *testing.T) {
	root := map[string]any{"item_list": []any{
		map[string]any{"desc": "第一条", "video": map[string]any{}},
		map[string]any{"desc": "第二条", "video": map[string]any{}},
	}}
	if got := str(findMap(root, []string{"desc"}, []string{"video"})["desc"]); got != "第一条" {
		t.Fatalf("应取数组第一项, 得到 %q", got)
	}
}

func TestFindMapRequiresKeys(t *testing.T) {
	root := map[string]any{"note": map[string]any{"desc": "有描述没视频"}}
	if got := findMap(root, []string{"desc"}, []string{"video"}); got != nil {
		t.Fatalf("不满足 anyOf 条件时不该命中: %+v", got)
	}
}

func TestFirstStringSearchesNested(t *testing.T) {
	root := map[string]any{"data": []any{map[string]any{"author": map[string]any{"nickname": "阿蓝"}}}}
	if got := firstString(root, "nickname"); got != "阿蓝" {
		t.Fatalf("拿到 %q", got)
	}
}

func TestCountAndTruncate(t *testing.T) {
	cases := map[int64]string{999: "999", 12000: "1.2万", 120000000: "1.2亿"}
	for n, want := range cases {
		if got := count(n); got != want {
			t.Fatalf("count(%d) = %q, want %q", n, got, want)
		}
	}
	if got := truncate("一二三四五", 3); got != "一二三…" {
		t.Fatalf("截断得到 %q", got)
	}
	if got := firstLine("第一行\n第二行"); got != "第一行" {
		t.Fatalf("取首行得到 %q", got)
	}
}

func TestNormalizeURL(t *testing.T) {
	if got := normalizeURL("//i0.hdslb.com/a.jpg"); got != "https://i0.hdslb.com/a.jpg" {
		t.Fatalf("协议相对地址没补全: %q", got)
	}
	if got := normalizeURL("https://x.com/a.jpg"); got != "https://x.com/a.jpg" {
		t.Fatalf("已是完整地址不该动: %q", got)
	}
}
