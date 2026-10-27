package browser

import (
	"strings"
	"testing"
)

func museDomain(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	return d == "muse.ai" || strings.HasSuffix(d, ".muse.ai")
}

func TestParseCookiesFormats(t *testing.T) {
	cases := map[string]struct {
		raw   string
		names []string
	}{
		"cookie-editor": {`[{"name":"sid","value":"1","domain":".muse.ai","httpOnly":true,"expirationDate":1999999999},{"name":"x","value":"2","domain":".google.com"}]`, []string{"sid"}},
		"wrapped":       {`{"cookies":[{"name":"sid","value":"1","domain":"muse.ai"}]}`, []string{"sid"}},
		"pairs":         {`{"sid":"1","n":2}`, []string{"n", "sid"}},
		"netscape":      {"# Netscape\n#HttpOnly_.muse.ai\tTRUE\t/\tTRUE\t1999999999\tsid\t1\n.evil.com\tTRUE\t/\tTRUE\t0\tbad\t2", []string{"sid"}},
		"header":        {"Cookie: sid=1; theme=dark", []string{"sid", "theme"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cookies, err := ParseCookies(c.raw, ".muse.ai", museDomain)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, ck := range cookies {
				if !museDomain(ck.Domain) || ck.Path == "" {
					t.Fatalf("cookie %+v kept outside muse.ai or without path", ck)
				}
				got[ck.Name] = true
			}
			if len(got) != len(c.names) {
				t.Fatalf("got %v, want %v", got, c.names)
			}
			for _, n := range c.names {
				if !got[n] {
					t.Fatalf("missing %s in %v", n, got)
				}
			}
		})
	}
	for _, raw := range []string{"", "[not json", `[{"name":"a","value":"1","domain":".google.com"}]`} {
		if _, err := ParseCookies(raw, ".muse.ai", museDomain); err == nil {
			t.Errorf("ParseCookies(%q) accepted", raw)
		}
	}
}
