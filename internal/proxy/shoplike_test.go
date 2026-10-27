package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestShoplikeUsesHTTPSAndReadsItsReplies(t *testing.T) {
	if !strings.HasPrefix(shoplikeAPI, "https://") {
		t.Fatalf("shoplikeAPI = %q: the key must not cross the network in clear", shoplikeAPI)
	}
	var gotPath, gotToken string
	reply := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken = r.URL.Path, r.URL.Query().Get("access_token")
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	old := shoplikeAPI
	shoplikeAPI = srv.URL + "/Api/"
	defer func() { shoplikeAPI = old }()

	p := Parse("shoplike:sl-key-789")
	cases := []struct {
		name, reply string
		requestNew  bool
		wantPath    string
		want        Status
	}{
		{"live", `{"status":"success","data":{"proxy":"1.2.3.4:8080"}}`, false, "/Api/getCurrentProxy", StatusLive},
		{"new", `{"status":"success","data":{"proxy":"1.2.3.4:8080"}}`, true, "/Api/getNewProxy", StatusLive},
		{"expired key, no diacritics", `{"status":"error","mess":"Key khong ton tai hoac da het han"}`, false, "/Api/getCurrentProxy", StatusLimitReached},
		{"too soon", `{"status":"error","mess":"Con 30 giay nua moi doi duoc proxy"}`, true, "/Api/getNewProxy", StatusRateLimited},
	}
	for _, tc := range cases {
		reply = tc.reply
		res := Check(context.Background(), p, tc.requestNew, 5*time.Second)
		if gotPath != tc.wantPath || gotToken != "sl-key-789" {
			t.Errorf("%s: called %s with token %q", tc.name, gotPath, gotToken)
		}
		if res.Status != tc.want {
			t.Errorf("%s: status = %s (%s), want %s", tc.name, res.Status, res.Message, tc.want)
		}
		if strings.Contains(res.Message, "sl-key-789") {
			t.Errorf("%s: message leaks the key: %s", tc.name, res.Message)
		}
	}
}
