package main

import (
	"context"
	"os"
	"testing"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/events"
)

// Temporary: seed every saved cookie of an account into its running profile
// (no domain filter), then check it.
func TestLiveCheckTmp(t *testing.T) {
	id := os.Getenv("SNAKEMEDIA_LIVE_CHECK")
	if id == "" {
		t.Skip()
	}
	info, _ := buildinfo.Parse(manifest)
	a, err := NewApp(info)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	a.ctx = ctx
	a.accounts.Start(ctx, func(events.Event, any) {})
	defer a.shutdown(ctx)
	rec, _ := a.accounts.Record(id)
	br, err := a.accounts.Browser(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("profile %s, %d saved cookies", rec.ProfileDir, len(rec.Cookies))
	if err := browser.InjectCookies(ctx, br.Port, a.cfg.Get().Accounts, rec.Cookies); err != nil {
		t.Fatal(err)
	}
	res := a.accounts.Check(ctx, id)
	t.Logf("CHECK ok=%v status=%s identity=%s msg=%q err=%q", res.Success, res.Data.Status, res.Data.Identity, res.Data.Message, res.Error)
}
