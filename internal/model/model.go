package model

type Platform struct {
	Name        string
	DisplayName string
}

type Author struct {
	Name   string
	ID     string
	Avatar string
	Desc   string
}

type Stats struct {
	View    string
	Like    string
	Collect string
	Share   string
	Comment string
	Extra   [][2]string
}

type Content interface{ content() }

type Text struct{ Value string }

type Image struct{ URL string }

type Video struct {
	URL      string
	Cover    string
	Duration float64
	Headers  map[string]string
}

type Audio struct {
	URL      string
	Title    string
	Artist   string
	Duration float64
	Headers  map[string]string
}

type LivePhoto struct {
	Image   string
	Stream  string
	Headers map[string]string
}

type Link struct {
	URL   string
	Title string
	Site  string
	Desc  string
}

func (Text) content()      {}
func (Image) content()     {}
func (Video) content()     {}
func (Audio) content()     {}
func (LivePhoto) content() {}
func (Link) content()      {}

type Result struct {
	Platform  Platform
	URL       string
	Title     string
	Author    Author
	Stats     Stats
	Timestamp int64
	Content   []Content
	Repost    *Result
}

func (r *Result) Videos() []Video {
	out := make([]Video, 0, len(r.Content))
	for _, item := range r.Content {
		if v, ok := item.(Video); ok && v.URL != "" {
			out = append(out, v)
		}
	}
	return out
}

func (r *Result) Audios() []Audio {
	out := make([]Audio, 0, len(r.Content))
	for _, item := range r.Content {
		if a, ok := item.(Audio); ok && a.URL != "" {
			out = append(out, a)
		}
	}
	return out
}

func (r *Result) Images() []string {
	out := make([]string, 0, len(r.Content))
	for _, item := range r.Content {
		if img, ok := item.(Image); ok && img.URL != "" {
			out = append(out, img.URL)
		}
	}
	return out
}

func (r *Result) Texts() []string {
	out := make([]string, 0, len(r.Content))
	for _, item := range r.Content {
		if t, ok := item.(Text); ok && t.Value != "" {
			out = append(out, t.Value)
		}
	}
	return out
}

func (r *Result) Cover() string {
	for _, item := range r.Content {
		switch v := item.(type) {
		case Video:
			if v.Cover != "" {
				return v.Cover
			}
		case Image:
			if v.URL != "" {
				return v.URL
			}
		}
	}
	return ""
}
