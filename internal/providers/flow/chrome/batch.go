package chrome

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/config"
)

// FlowClient sends batchexecute RPCs with the same runtime parameters, headers and
// cookie scope as the credits call, using credentials captured from the account's
// own browser profile. Cookies rotated by the provider are kept for persistence.
// A FlowClient is not safe for concurrent use.
type FlowClient struct {
	base         string
	origin       *url.URL
	creds        Credentials
	policy       config.AccountConfig
	uploadPolicy config.FlowConfig
	client       *http.Client
}

// NewFlowClient prepares a client for FlowURL.
func NewFlowClient(c Credentials, policy config.AccountConfig) (*FlowClient, error) {
	return newFlowClient(FlowURL, c, policy)
}

func newFlowClient(base string, c Credentials, policy config.AccountConfig) (*FlowClient, error) {
	if err := c.validateSession(); err != nil {
		return nil, err
	}
	origin, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	c.Cookies = append([]CookieItem(nil), c.Cookies...)
	jar, err := flowCookieJar(origin, c.Cookies)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return &FlowClient{base: base, origin: origin, creds: c, policy: policy, uploadPolicy: config.DefaultFlowConfig(), client: client}, nil
}

// Credentials returns the session credentials including any rotated cookies.
func (f *FlowClient) Credentials() Credentials {
	c := f.creds
	c.Cookies = append([]CookieItem(nil), f.creds.Cookies...)
	return c
}

// Do posts fReq for rpcID within timeout and returns the bounded response body.
// A non-200 status returns the body together with an *HTTPError, so callers can
// still inspect provider error markers.
func (f *FlowClient) Do(ctx context.Context, rpcID, fReq string, timeout time.Duration) ([]byte, error) {
	if !rpcIDPattern.MatchString(rpcID) {
		return nil, errors.New("invalid RPC ID")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body := url.Values{"f.req": {fReq}, "at": {f.creds.AtToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.base+batchPath+"?"+batchParams(f.creds, rpcID).Encode(), strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	setBatchHeaders(req, f.creds, f.base)
	resp, err := f.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// net/url errors include the request URL and its session query parameters.
		return nil, errors.New("Không kết nối được API Flow. Kiểm tra mạng hoặc thử lại sau.")
	}
	defer resp.Body.Close()
	f.creds.Cookies = rotateCookies(f.creds.Cookies, f.origin, resp)
	data, err := readBounded(resp.Body, f.policy.MaxResponseBytes)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return data, responseError(resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	return data, nil
}

// FetchMedia downloads a generated media file served by Google's content hosts,
// sending the session's cookies only where their scope allows. The response must
// carry a Content-Type starting with one of the accepted prefixes.
func (f *FlowClient) FetchMedia(ctx context.Context, rawURL string, accept []string, limit int64, timeout time.Duration) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || !mediaHost(u.Hostname()) {
		return nil, "", errors.New("URL ảnh không thuộc máy chủ nội dung của Google")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", f.creds.UserAgent)
	req.Header.Set("Referer", f.base+"/")
	client := &http.Client{Jar: f.client.Jar}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", errors.New("Không kết nối được máy chủ ảnh của Google.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", responseError(resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	contentType := resp.Header.Get("Content-Type")
	if !slices.ContainsFunc(accept, func(prefix string) bool { return strings.HasPrefix(contentType, prefix) }) {
		return nil, "", fmt.Errorf("máy chủ trả về %q thay vì %s", contentType, strings.Join(accept, ", "))
	}
	data, err := readBounded(resp.Body, limit)
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

func mediaHost(host string) bool {
	return strings.HasSuffix(host, ".google") || host == "storage.googleapis.com" || strings.HasSuffix(host, ".googleusercontent.com")
}

func (f *FlowClient) SetUploadPolicy(policy config.FlowConfig) { f.uploadPolicy = policy }
