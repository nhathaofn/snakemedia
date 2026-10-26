package muse

import (
	"errors"
	"testing"
	"time"

	"SnakeMedia/internal/config"
)

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
			cookies, err := ParseCookies(c.raw)
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
		if _, err := ParseCookies(raw); err == nil {
			t.Errorf("ParseCookies(%q) accepted", raw)
		}
	}
}

func TestParseQuota(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	if parseQuota(nil, now) != nil || parseQuota(&subscription{}, now) != nil {
		t.Fatal("empty subscription should give no quota")
	}
	sub := &subscription{TopupBalance: 5, TopupTotal: 10}
	sub.Usage = &struct {
		PercentUsed float64 `json:"percentUsed"`
		ResetsAt    float64 `json:"resetsAt"`
		QuotaStatus string  `json:"quotaStatus"`
	}{PercentUsed: 100, ResetsAt: float64(now.Add(48 * time.Hour).Unix()), QuotaStatus: "SUFFICIENT"}
	q := parseQuota(sub, now)
	if !q.weeklyOut() || !q.topupLeft() || !q.resetsAt().Equal(now.Add(48*time.Hour)) {
		t.Fatalf("quota = %+v", q)
	}
	if (&MuseQuota{PercentUsed: 40, Status: "LOW"}).weeklyOut() || !(&MuseQuota{PercentUsed: 40, Status: "EXHAUSTED"}).weeklyOut() {
		t.Fatal("status classification wrong")
	}
}

func TestClassifyReply(t *testing.T) {
	var usage *usageError
	var policy *policyError
	var muse *museError
	if !errors.As(classifyReply("Bạn đã hết lượt tạo video hôm nay", "video"), &usage) {
		t.Error("usage reply not recognised")
	}
	if !errors.As(classifyReply("I can't create that, it violates our content policy.", "video"), &policy) {
		t.Error("policy reply not recognised")
	}
	if !errors.As(classifyReply("Công cụ đang bận", "video"), &muse) || !errors.As(classifyReply("", "video"), &muse) {
		t.Error("plain reply should be a Muse error")
	}
}

func TestPausedUntil(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	reset := now.Add(24 * time.Hour)
	out := &MuseQuota{PercentUsed: 100, ResetsAt: reset.Format(time.RFC3339)}
	acc := MuseAccount{Quota: out}
	if got := pausedUntil(acc, cfg, now); !got.Equal(reset) {
		t.Fatalf("paused until %v, want %v", got, reset)
	}
	acc.Quota = &MuseQuota{PercentUsed: 100, ResetsAt: reset.Format(time.RFC3339), TopupBalance: 1, TopupTotal: 5}
	if !pausedUntil(acc, cfg, now).IsZero() {
		t.Fatal("top-up tokens should keep the account working")
	}
	cfg.KeepGoing = true
	acc.Quota = out
	if !pausedUntil(acc, cfg, now).IsZero() {
		t.Fatal("keepGoing should ignore the quota")
	}
	cfg.KeepGoing = false
	if !pausedUntil(acc, cfg, reset.Add(time.Minute)).IsZero() {
		t.Fatal("a past reset makes the quota stale")
	}
	acc = MuseAccount{PausedUntil: now.Add(time.Hour).Format(time.RFC3339)}
	if pausedUntil(acc, cfg, now).IsZero() {
		t.Fatal("manual pause ignored")
	}
}

func TestPlan(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	cfg.MaxAccounts, cfg.TotalThreads = 3, 3
	now := time.Now()
	acc := func(id string, pct float64) MuseAccount {
		return MuseAccount{ID: id, Enabled: true, Status: MuseAccountSignedIn, Quota: &MuseQuota{PercentUsed: pct}}
	}
	accounts := []MuseAccount{acc("a", 80), acc("b", 10), acc("c", 50), {ID: "off", Enabled: false, Status: MuseAccountSignedIn}}
	jobs := []MuseJob{
		{ID: "q1", Status: MuseJobQueued, CreatedAt: "1"},
		{ID: "q2", Status: MuseJobQueued, CreatedAt: "2"},
		{ID: "sent", Status: MuseJobProcessing, SessionID: "s", AccountID: "a", CreatedAt: "0"},
		{ID: "done", Status: MuseJobSucceeded, CreatedAt: "0"},
	}
	got := plan(jobs, accounts, cfg, map[string]string{}, nil, now)
	want := []assignment{{"sent", "a"}, {"q1", "b"}, {"q2", "c"}}
	if len(got) != len(want) {
		t.Fatalf("plan = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("plan = %+v, want %+v", got, want)
		}
	}

	cfg.Mode = config.MuseModeSequential
	got = plan(jobs[:2], accounts, cfg, map[string]string{"x": "a"}, nil, now)
	if len(got) != 2 || got[0].accountID != "b" || got[1].accountID != "c" {
		t.Fatalf("sequential plan = %+v", got)
	}

	cfg.TotalThreads = 1
	if got := plan(jobs[:2], accounts, cfg, map[string]string{"x": "a"}, nil, now); len(got) != 0 {
		t.Fatalf("total thread limit ignored: %+v", got)
	}
}

func TestPlanSkipsHeldAndPausedProjects(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	cfg.MaxAccounts, cfg.TotalThreads = 2, 2
	now := time.Now()
	accounts := []MuseAccount{{ID: "a", Enabled: true, Status: MuseAccountSignedIn}}
	jobs := []MuseJob{
		{ID: "held", Status: MuseJobQueued, Held: true, CreatedAt: "1"},
		{ID: "paused", Status: MuseJobQueued, ProjectID: "proj-p", CreatedAt: "2"},
		{ID: "ok", Status: MuseJobQueued, ProjectID: "proj-live", CreatedAt: "3"},
	}
	got := plan(jobs, accounts, cfg, map[string]string{}, map[string]bool{"proj-p": true}, now)
	if len(got) != 1 || got[0].jobID != "ok" {
		t.Fatalf("expected only 'ok' job to be planned, got: %+v", got)
	}
}

func TestPlanPinnedAccount(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	cfg.MaxAccounts, cfg.TotalThreads = 2, 2
	now := time.Now()
	accounts := []MuseAccount{
		{ID: "a", Enabled: true, Status: MuseAccountSignedIn},
		{ID: "b", Enabled: true, Status: MuseAccountSignedIn},
	}
	jobs := []MuseJob{
		{ID: "pinned", Status: MuseJobQueued, Thread: "t1", PinAccount: "b", CreatedAt: "1"},
	}
	got := plan(jobs, accounts, cfg, map[string]string{}, nil, now)
	if len(got) != 1 || got[0].accountID != "b" {
		t.Fatalf("expected pinned to 'b', got: %+v", got)
	}
}
