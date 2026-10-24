package chrome

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"SnakeMedia/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDirectHTTPUsesRuntimeContextAndScopedCookies(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	credentials := Credentials{RPCs: RPCRegistry{Credits: creditsRPC, Availability: availabilityRPC}, AtToken: "runtime-at", SessionID: "runtime-session", BuildLabel: "runtime-build", Language: "runtime-language", UserAgent: "runtime-agent", SourcePath: "/project/test", Cookies: []CookieItem{{Name: "valid", Value: "original", Domain: "flow.google.com", Path: "/", Secure: true}, {Name: "other", Value: "must-not-send", Domain: "accounts.google.com", Path: "/", Secure: true}, {Name: "expired", Value: "must-not-send", Domain: "flow.google.com", Path: "/", Expires: 1, Secure: true}}}
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !r.Close {
			t.Error("credits request connection must close instead of entering the idle pool")
		}
		if r.Header.Get("User-Agent") != credentials.UserAgent || r.URL.Query().Get("bl") != credentials.BuildLabel || r.URL.Query().Get("rpcids") != credentials.RPCs.ids() || r.Header.Get("Referer") != FlowURL+credentials.SourcePath {
			t.Error("request contains invented runtime context")
		}
		if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "valid=original") || strings.Contains(cookie, "must-not-send") {
			t.Error("cookie scope/expiry ignored")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "runtime-at") {
			t.Error("runtime CSRF missing")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Set-Cookie": []string{"valid=rotated; Path=/; Secure; HttpOnly"}}, Body: io.NopCloser(strings.NewReader(creditsFrame(`[0,1,2,2,null,58,null]`) + availabilityFrame(`[1]`))), Request: r}, nil
	})
	credits, next, err := QueryCreditsDirectHTTP(context.Background(), credentials, config.DefaultAccountConfig())
	if err != nil || credits.Credits != 0 || credits.AvailabilityCode == nil || *credits.AvailabilityCode != 1 {
		t.Fatalf("valid zero/availability response rejected: %v", err)
	}
	rotated := false
	for _, cookie := range next.Cookies {
		if cookie.Name == "valid" && cookie.Value == "rotated" {
			rotated = true
		}
	}
	if !rotated || credentials.Cookies[0].Value != "original" {
		t.Fatal("cookie rotation missing or input mutated")
	}
	credentials.BuildLabel = ""
	if _, _, err := QueryCreditsDirectHTTP(context.Background(), credentials, config.DefaultAccountConfig()); err == nil {
		t.Fatal("missing runtime build silently replaced")
	}
}
