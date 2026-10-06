// Package model 是解析层与发送层之间唯一的数据形态。
package model

// Media 一次解析的产物: 视频、图集、或只有文案的笔记。
type Media struct {
	Platform string
	Title    string
	Author   string
	Desc     string
	Cover    string
	Stat     string
	Duration int
	PageURL  string
	Video    *Video
	Images   []string
}

// Video 可下载的视频源。
type Video struct {
	URL     string
	Headers map[string]string
	Size    int64
}

// Kind 决定发送层怎么排版。
func (m Media) Kind() string {
	switch {
	case m.Video != nil:
		return "video"
	case len(m.Images) > 0:
		return "album"
	default:
		return "text"
	}
}

// Summary 一行简介: 正文里能当文案用的那句话。
func (m Media) Summary() string {
	if m.Desc != "" {
		return m.Desc
	}
	return m.Title
}
