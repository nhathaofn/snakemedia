package main

import (
	"context"
	"os"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/providers/muse"
)

// liveMuseSetup runs what follows a sign-in in the window on a signed-in Muse
// account (SNAKEMEDIA_LIVE_ACCOUNT, or the first one): the check (quota and
// limits), then the setup message in a chat of its own, waiting for Muse's
// reply, and saves the outcome on the account. It spends one Muse turn.
func liveMuseSetup(t *testing.T, a *App) {
	recs, err := a.accounts.Records(accounts.ProviderMuse)
	if err != nil {
		t.Fatal(err)
	}
	want := os.Getenv("SNAKEMEDIA_LIVE_ACCOUNT")
	var rec *accounts.Record
	for i := range recs {
		if recs[i].Enabled && recs[i].Status == accounts.StatusSignedIn && (want == "" || recs[i].ID == want) {
			rec = &recs[i]
			break
		}
	}
	if rec == nil {
		t.Fatal("no signed-in Muse account")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	defer cancel()
	b, err := a.accounts.Browser(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	site := muse.NewSite(a.cfg)
	var insp accounts.Inspection
	for i := 0; ; i++ {
		if insp, err = site.Inspect(ctx, b, *rec); err == nil && insp.SignedIn {
			break
		}
		if i == 20 {
			t.Fatalf("not signed in: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
	t.Logf("signed in as %s, usage %s", rec.Label(), dump(insp.Usage))
	started := time.Now()
	insp = site.AfterSignIn(ctx, b, *rec, insp)
	t.Logf("after %s: note=%q usage=%s", time.Since(started).Round(time.Second), insp.Note, dump(insp.Usage))
	t.Logf("data: %s", dump(insp.Data))
	if _, err := a.accounts.Update(rec.ID, func(r *accounts.Record) {
		r.Usage, r.Message = insp.Usage, insp.Note
		if err := r.SetData(insp.Data); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if insp.Note != "" {
		t.Fatalf("setup message not answered: %s", insp.Note)
	}
}

// liveMuseLimits checks every signed-in Muse account (sends nothing) and
// prints the attachment limits the create screens then offer.
func liveMuseLimits(t *testing.T, a *App) {
	recs, err := a.accounts.Records(accounts.ProviderMuse)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if !rec.Enabled || rec.Status != accounts.StatusSignedIn {
			continue
		}
		res := a.accounts.Check(a.ctx, rec.ID)
		t.Logf("check %s: ok=%v status=%s message=%q", rec.Label(), res.Success, res.Data.Status, res.Data.Message)
	}
	opts := a.muse.GetOptions()
	t.Logf("options: %s", dump(opts.Data))
	if !opts.Success || !opts.Data.Known {
		t.Fatal("Muse's attachment limits are still unknown")
	}
}
