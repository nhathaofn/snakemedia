package muse

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
)

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
	if (&MuseQuota{PercentUsed: 99.5}).weeklyOut() || (&MuseQuota{TopupTotal: 5}).topupLeft() {
		t.Fatal("quota classification wrong")
	}
}

func TestParseQuotaKeepsMuseLabels(t *testing.T) {
	var sub subscription
	raw := `{"tier":{"name":"Muse Free","isPaid":false},"usage":{"state":"METERED","percentUsed":5,"resetsAt":1793556612,"quotaStatus":"SUFFICIENT"},
		"statusSubtitle":"Weekly limit resets on Nov 1","usageRowLabel":"Free plan","usageRowValueLabel":"5% used",
		"topupBalance":1240000000,"topupTotal":1240000000,"topupRowLabel":"Additional tokens","topupRowValueLabel":"0% used (31B tokens left)"}`
	if err := json.Unmarshal([]byte(raw), &sub); err != nil {
		t.Fatal(err)
	}
	q := parseQuota(&sub, time.Now())
	if q.Tier != "Muse Free" || q.UsageValue != "5% used" || q.TopupValue != "0% used (31B tokens left)" || q.resetsAt().Unix() != 1793556612 {
		t.Fatalf("quota = %+v", q)
	}
	items := quotaUsage(q)
	if items[0].Label != "Free plan" || items[0].Value != "5% used" || items[2].Label != "Additional tokens" || items[3].Value != "Muse Free" {
		t.Fatalf("usage = %+v", items)
	}
}

func TestParseCapabilities(t *testing.T) {
	var raw rawCapabilities
	in := `{"upload":{"maxFileSize":41943040,"maxFilesPerUpload":10,"supportedMimeTypes":[
		{"mimeType":"image/jpeg","extensions":[".jpg",".jpeg"],"category":"image"},
		{"mimeType":"image/png","extensions":[".PNG"],"category":"image"},
		{"mimeType":"video/mp4","extensions":[".mp4"],"category":"video"}]},
		"models":{"selected_model":"auto/auto","available_models":[{"id":"auto/auto","display_name":"Auto"}]}}`
	if err := json.Unmarshal([]byte(in), &raw); err != nil {
		t.Fatal(err)
	}
	c := parseCapabilities(raw, time.Now())
	if c.MaxFileBytes != 40<<20 || c.MaxFiles != 10 || len(c.ImageTypes) != 2 || c.Model != "auto/auto" || len(c.Models) != 1 {
		t.Fatalf("caps = %+v", c)
	}
	if mime, ok := c.imageMime(".png"); !ok || mime != "image/png" {
		t.Fatal("extension case not normalised")
	}
	if _, ok := c.imageMime(".mp4"); ok {
		t.Fatal("a video type was taken as an image")
	}
	if got := c.extensions(); len(got) != 3 || got[0] != ".jpeg" {
		t.Fatalf("extensions = %v", got)
	}
	if parseCapabilities(rawCapabilities{}, time.Now()) != nil {
		t.Fatal("capabilities without upload limits")
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
	cfg.UseTopup = false
	if got := pausedUntil(acc, cfg, now); !got.Equal(reset) {
		t.Fatalf("top-up tokens used although turned off: %v", got)
	}
	cfg.UseTopup = true
	acc.Quota = out
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
	now := time.Now()
	acc := func(id string, pct float64) MuseAccount {
		return MuseAccount{ID: id, Enabled: true, Status: accounts.StatusSignedIn, Quota: &MuseQuota{PercentUsed: pct}}
	}
	accounts := []MuseAccount{acc("a", 80), acc("b", 10), acc("c", 50), {ID: "off", Enabled: false, Status: accounts.StatusSignedIn}}
	jobs := []MuseJob{
		{ID: "q1", Status: MuseJobQueued, CreatedAt: "1"},
		{ID: "q2", Status: MuseJobQueued, CreatedAt: "2"},
		{ID: "sent", Status: MuseJobProcessing, SessionID: "s", AccountID: "a", CreatedAt: "0"},
		{ID: "done", Status: MuseJobSucceeded, CreatedAt: "0"},
	}
	got := plan(jobs, accounts, cfg, map[string]string{}, now)
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
	got = plan(jobs[:2], accounts, cfg, map[string]string{"x": "a"}, now)
	if len(got) != 2 || got[0].accountID != "b" || got[1].accountID != "c" {
		t.Fatalf("sequential plan = %+v", got)
	}

	// Every account at its thread limit: nothing more starts.
	if got := plan(jobs[:2], accounts, cfg, map[string]string{"x": "a", "y": "b", "z": "c"}, now); len(got) != 0 {
		t.Fatalf("thread limit ignored: %+v", got)
	}
}

// Every usable account takes work, however many there are; no count is set.
func TestPlanUsesEveryUsableAccount(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	now := time.Now()
	ok := func(id string) MuseAccount {
		return MuseAccount{ID: id, Enabled: true, Status: accounts.StatusSignedIn}
	}
	accs := []MuseAccount{ok("a"), ok("b"), ok("c"), ok("d"), ok("e")}
	accs = append(accs,
		MuseAccount{ID: "out", Enabled: true, Status: accounts.StatusSignedOut},
		MuseAccount{ID: "noproxy", Enabled: true, Status: accounts.StatusSignedIn, NoRoute: "proxy chết"},
		MuseAccount{ID: "resting", Enabled: true, Status: accounts.StatusSignedIn, PausedUntil: now.Add(time.Hour).Format(time.RFC3339)},
	)
	var jobs []MuseJob
	for i := range 10 {
		jobs = append(jobs, MuseJob{ID: fmt.Sprint("q", i), Status: MuseJobQueued, CreatedAt: fmt.Sprint(i)})
	}
	got := plan(jobs, accs, cfg, map[string]string{}, now)
	used := map[string]bool{}
	for _, a := range got {
		used[a.accountID] = true
	}
	if len(got) != 5 || len(used) != 5 || used["out"] || used["noproxy"] || used["resting"] {
		t.Fatalf("plan = %+v", got)
	}
}

func TestPlanSkipsHeldJobs(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	accs := []MuseAccount{{ID: "a", Enabled: true, Status: accounts.StatusSignedIn}}
	jobs := []MuseJob{
		{ID: "held", Status: MuseJobQueued, Held: true, CreatedAt: "1"},
		{ID: "ok", Status: MuseJobQueued, CreatedAt: "2"},
	}
	got := plan(jobs, accs, cfg, map[string]string{}, time.Now())
	if len(got) != 1 || got[0].jobID != "ok" {
		t.Fatalf("plan = %+v", got)
	}
}

func TestPlanSharesByQuotaLeft(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	acc := func(id string, pct float64, sentAgo time.Duration) MuseAccount {
		return MuseAccount{
			ID: id, Enabled: true, Status: accounts.StatusSignedIn,
			Quota:      &MuseQuota{PercentUsed: pct},
			LastSentAt: now.Add(-sentAgo).Format(time.RFC3339Nano),
		}
	}
	jobs := []MuseJob{{ID: "q", Status: MuseJobQueued, CreatedAt: "1"}}
	pick := func(accs ...MuseAccount) string {
		got := plan(jobs, accs, cfg, map[string]string{}, now)
		if len(got) != 1 {
			t.Fatalf("plan = %+v", got)
		}
		return got[0].accountID
	}

	// The lowest usage no longer takes every job: the account it just used
	// gives way to one rested long enough for its share.
	if got := pick(acc("low", 10, 20*time.Second), acc("mid", 30, 5*time.Minute)); got != "mid" {
		t.Fatalf("picked %s, want the rested account", got)
	}
	// With equal rest, the account with more room left goes first.
	if got := pick(acc("low", 10, time.Minute), acc("mid", 30, time.Minute)); got != "low" {
		t.Fatalf("picked %s, want the account with more room", got)
	}
	// An account that never sent goes first.
	fresh := acc("new", 50, 0)
	fresh.LastSentAt = ""
	if got := pick(acc("low", 10, time.Hour), fresh); got != "new" {
		t.Fatalf("picked %s, want the unused account", got)
	}
	// Additional (paid) tokens are spent only when no weekly usage is free.
	topup := acc("topup", 100, 24*time.Hour)
	topup.Quota.TopupBalance, topup.Quota.TopupTotal = 900, 1000
	if got := pick(topup, acc("busy", 95, 20*time.Second)); got != "busy" {
		t.Fatalf("picked %s, want the account still on weekly usage", got)
	}
}

func TestPlanRotatesOverManyJobs(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	accs := []MuseAccount{
		{ID: "a", Enabled: true, Status: accounts.StatusSignedIn, Quota: &MuseQuota{PercentUsed: 10}},
		{ID: "b", Enabled: true, Status: accounts.StatusSignedIn, Quota: &MuseQuota{PercentUsed: 55}},
	}
	jobs := []MuseJob{{ID: "q", Status: MuseJobQueued, CreatedAt: "1"}}
	count := map[string]int{}
	for range 100 {
		got := plan(jobs, accs, cfg, map[string]string{}, now)
		if len(got) != 1 {
			t.Fatalf("plan = %+v", got)
		}
		count[got[0].accountID]++
		for i := range accs {
			if accs[i].ID == got[0].accountID {
				accs[i].LastSentAt = now.Format(time.RFC3339Nano)
			}
		}
		now = now.Add(time.Minute)
	}
	// Room left is 0.9 against 0.45: about two jobs to one, never all on one.
	if count["b"] < 25 || count["a"] < 55 {
		t.Fatalf("shares = %v", count)
	}
}
