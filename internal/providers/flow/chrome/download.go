package chrome

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

func validMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && mediaHost(u.Hostname())
}

// DownloadMedia keeps large video bytes outside Go/Wails memory envelopes.
func (f *FlowClient) DownloadMedia(ctx context.Context, rawURL string, accept []string, timeout time.Duration, sink func(io.Reader, string) error) error {
	if !validMediaURL(rawURL) {
		return errors.New("URL video không thuộc máy chủ nội dung Google.")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", f.creds.UserAgent)
	req.Header.Set("Referer", f.base+"/")
	client := &http.Client{Jar: f.client.Jar, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !validMediaURL(next.URL.String()) {
			return errors.New("Video chuyển hướng đến địa chỉ không hợp lệ.")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Không kết nối được máy chủ video của Google.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	contentType := resp.Header.Get("Content-Type")
	if !slices.ContainsFunc(accept, func(prefix string) bool { return strings.HasPrefix(contentType, prefix) }) {
		return errors.New("Máy chủ không trả về tệp video.")
	}
	return sink(resp.Body, contentType)
}
