package htmlx

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/model"
)

var newlineTags = map[string]bool{
	"p": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "li": true, "blockquote": true, "pre": true, "hr": true,
}

func Num(value int64) string {
	if value < 10000 {
		return strconv.FormatInt(value, 10)
	}
	return fmt.Sprintf("%.1f万", float64(value)/10000)
}

func Time(value string) int64 {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05",
		"2006-01-02 15:04", "2006-01-02T15:04:05.999999", "2006-01-02",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Unix()
		}
	}
	return 0
}

func Compact(value string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(value), " "))
}

type Media struct {
	Text   string
	Images []string
}

func Rich(raw string) Media {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return Media{Text: Compact(raw)}
	}

	var (
		out  Media
		buf  strings.Builder
		seen = map[*html.Node]bool{}
	)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if newlineTags[n.Data] {
				buf.WriteString("\n")
			}
			if n.Data == "img" {
				if src := attr(n, "src"); src != "" {
					out.Images = append(out.Images, src)
				}
			}
			if n.Data == "a" {
				if !seen[n] {
					seen[n] = true
					if text := nodeText(n); text != "" {
						buf.WriteString(text)
					}
				}
				return
			}
		}
		if n.Type == html.TextNode {
			if text := Compact(n.Data); text != "" {
				buf.WriteString(text)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	out.Text = Compact(buf.String())
	return out
}

func Text(raw string) string { return Rich(raw).Text }

func nodeText(n *html.Node) string {
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			if text := Compact(cur.Data); text != "" {
				buf.WriteString(text)
			}
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return Compact(buf.String())
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func Images(media Media, headers map[string]string) []model.Content {
	out := make([]model.Content, 0, len(media.Images))
	for _, url := range media.Images {
		out = append(out, model.Image{URL: url})
	}
	_ = headers
	return out
}
