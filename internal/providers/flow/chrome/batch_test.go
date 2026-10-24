package chrome

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
)

func flowTestCredentials() Credentials {
	return Credentials{AtToken: "runtime-at", SessionID: "runtime-session", BuildLabel: "runtime-build", Language: "vi", UserAgent: "runtime-agent", SourcePath: "/project/test", AuthUser: "1", Cookies: []CookieItem{
		{Name: "SID", Value: "original", Domain: ".google.com", Path: "/", Secure: true},
		{Name: "other", Value: "must-not-send", Domain: "accounts.google.com", Path: "/", Secure: true},
		{Name: "foreign", Value: "must-not-send", Domain: ".example.com", Path: "/", Secure: true},
	}}
}

func TestFlowClientSendsRuntimeSessionContext(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	c := flowTestCredentials()
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !r.Close {
			t.Error("batchexecute connection must close instead of entering the idle pool")
		}
		q := r.URL.Query()
		for key, want := range map[string]string{"rpcids": "HTrJv", "bl": c.BuildLabel, "f.sid": c.SessionID, "hl": c.Language, "source-path": c.SourcePath, "authuser": c.AuthUser, "rt": "c"} {
			if q.Get(key) != want {
				t.Errorf("query %s = %q, want %q", key, q.Get(key), want)
			}
		}
		if r.Header.Get("User-Agent") != c.UserAgent || r.Header.Get("Referer") != FlowURL+c.SourcePath || r.Header.Get("X-Same-Domain") != "1" {
			t.Error("request headers do not come from the browser session")
		}
		if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "SID=original") || strings.Contains(cookie, "must-not-send") {
			t.Errorf("cookie scope ignored: %s", cookie)
		}
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("at") != c.AtToken || form.Get("f.req") != `[[["HTrJv","[]",null,"generic"]]]` {
			t.Errorf("unexpected body: %s", body)
		}
		header := http.Header{"Set-Cookie": {"SID=rotated; Domain=.google.com; Path=/; Secure"}}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(`)]}'` + "\n[]")), Request: r}, nil
	})

	client, err := NewFlowClient(c, config.DefaultAccountConfig())
	if err != nil {
		t.Fatal(err)
	}
	body, err := client.Do(context.Background(), "HTrJv", `[[["HTrJv","[]",null,"generic"]]]`, time.Second)
	if err != nil || !strings.HasPrefix(string(body), ")]}'") {
		t.Fatalf("unexpected result %q %v", body, err)
	}
	rotated := false
	for _, cookie := range client.Credentials().Cookies {
		rotated = rotated || (cookie.Name == "SID" && cookie.Value == "rotated")
	}
	if !rotated {
		t.Fatal("rotated cookie not kept for persistence")
	}
}

func TestFlowClientReportsHTTPStatusWithBody(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		header := http.Header{"Retry-After": {"7"}}
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: header, Body: io.NopCloser(strings.NewReader("PUBLIC_ERROR_UNUSUAL_ACTIVITY")), Request: r}, nil
	})
	client, err := NewFlowClient(flowTestCredentials(), config.DefaultAccountConfig())
	if err != nil {
		t.Fatal(err)
	}
	body, err := client.Do(context.Background(), "ogiZ0b", "[]", time.Second)
	var remote *HTTPError
	if !errors.As(err, &remote) || remote.Status != http.StatusTooManyRequests || remote.RetryAfter != 7*time.Second {
		t.Fatalf("status not reported: %v", err)
	}
	if string(body) != "PUBLIC_ERROR_UNUSUAL_ACTIVITY" {
		t.Fatalf("error body not returned: %q", body)
	}
}

func TestFlowClientRequiresSessionValues(t *testing.T) {
	c := flowTestCredentials()
	c.SessionID = ""
	if _, err := NewFlowClient(c, config.DefaultAccountConfig()); err == nil {
		t.Fatal("expected missing session values to be rejected")
	}
	c = flowTestCredentials()
	c.Cookies = c.Cookies[1:]
	if _, err := NewFlowClient(c, config.DefaultAccountConfig()); err == nil {
		t.Fatal("expected a session without provider cookies to be rejected")
	}
}

func TestFlowClientFetchMedia(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("sig") != "abc" {
			t.Errorf("signed query altered: %s", r.URL.RawQuery)
		}
		if strings.Contains(r.Header.Get("Cookie"), "SID=") {
			t.Error("google.com session cookie sent to another site")
		}
		contentType := "image/png"
		if strings.Contains(r.URL.Path, "html") {
			contentType = "text/html"
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader("PNG")), Request: r}, nil
	})
	client, err := NewFlowClient(flowTestCredentials(), config.DefaultAccountConfig())
	if err != nil {
		t.Fatal(err)
	}
	data, contentType, err := client.FetchMedia(context.Background(), "https://flow-content.google/image/img-1?sig=abc", []string{"image/"}, 1<<20, time.Second)
	if err != nil || string(data) != "PNG" || contentType != "image/png" {
		t.Fatalf("unexpected result %q %q %v", data, contentType, err)
	}
	if _, _, err := client.FetchMedia(context.Background(), "https://flow-content.google/image/html?sig=abc", []string{"image/"}, 1<<20, time.Second); err == nil {
		t.Fatal("a non-image response must be rejected")
	}
	for _, foreign := range []string{"https://example.com/image/x", "http://flow-content.google/image/x", "https://flow-content.google.example.com/image/x"} {
		if _, _, err := client.FetchMedia(context.Background(), foreign, []string{"image/"}, 1<<20, time.Second); err == nil {
			t.Errorf("fetched media from %s", foreign)
		}
	}
}
