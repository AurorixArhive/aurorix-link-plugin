// Package media 负责把视频落到本地: QQ 的富媒体上传只认公网直链或本地文件,
// 而这些平台的直链带防盗链, 让平台服务器去拉多半被拒, 所以先自己下回来再传。
package media

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var ErrTooLarge = errors.New("视频超过体积上限")

// Download 把视频下到临时文件, 返回路径、字节数。
//
// 这里不走框架的请求客户端: 它是一次性把响应读进内存的, 几十上百兆的视频会把内存顶爆。
// 下载必须边读边写盘, 所以单独用一个只做下载的客户端。
func Download(rawURL string, headers map[string]string, limitBytes int64) (string, int64, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: downloadBudget(limitBytes)}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("下载视频失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("下载视频失败: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limitBytes {
		return "", 0, ErrTooLarge
	}

	file, err := os.CreateTemp("", "video-plugin-*.mp4")
	if err != nil {
		return "", 0, err
	}
	written, err := io.Copy(file, io.LimitReader(resp.Body, limitBytes+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file.Name())
		return "", 0, fmt.Errorf("下载视频失败: %w", err)
	}
	if written > limitBytes {
		os.Remove(file.Name())
		return "", 0, ErrTooLarge
	}
	return file.Name(), written, nil
}

// downloadBudget 整个下载的预算。按体积上限推: 固定 3 分钟在慢上行(移动网络)上
// 传不完 50MB, 而超时是上限不是目标 —— 文件小自然早结束。
func downloadBudget(limitBytes int64) time.Duration {
	const (
		floor   = 2 * time.Minute
		ceiling = 20 * time.Minute
		rate    = 64 << 10 // 保守按 64 KiB/s 估
	)
	budget := floor + time.Duration(limitBytes/rate)*time.Second
	return min(max(budget, floor), ceiling)
}

// PubliclyFetchable 判断这条直链能不能被"不带任何请求头"的第三方拉走 —— 也就是平台服务器
// 能不能自己去下。
//
// 实测: B站的 upos 无 Referer 直接 403(带上 Referer 才 206), 抖音/最右/米游社都不挑。
// 能直传就把 URL 交给 QQ, 省掉云手机上"下载 + 上传"两条腿 —— 那是整条链路里最慢的一段。
func PubliclyFetchable(rawURL string, timeout time.Duration) bool {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	// 只取 1 字节: 要的是"能不能拿到", 不是内容本身。
	req.Header.Set("Range", "bytes=0-0")
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// ProbeSize 先问体积, 超限的就不下载了。
// 站点对 HEAD 的回应不统一, 返回 0 表示不知道 —— 调用方照旧下载, 由下载环节兜底。
func ProbeSize(rawURL string, headers map[string]string, timeout time.Duration) int64 {
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return 0
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.ContentLength > 0 {
		return resp.ContentLength
	}
	if size, err := contentRangeSize(resp.Header.Get("Content-Range")); err == nil {
		return size
	}
	return 0
}

func contentRangeSize(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("没有 Content-Range")
	}
	_, total, ok := strings.Cut(value, "/")
	if !ok {
		return 0, errors.New("Content-Range 格式不对")
	}
	var size int64
	if _, err := fmt.Sscanf(total, "%d", &size); err != nil {
		return 0, err
	}
	return size, nil
}
