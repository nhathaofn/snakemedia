package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/events"
)

// TestLiveFreshProfile copies a signed-in account's cookies into a new
// account, which gets a new, empty Chrome profile, so a refusal tied to the
// old profile can be told from one tied to the Google account. Headless, no
// window. SNAKEMEDIA_LIVE_FRESH=<email or account ID>; the copy is named
// "<name> (profile mới)" and left for the user to keep or remove.
func TestLiveFreshProfile(t *testing.T) {
	who := os.Getenv("SNAKEMEDIA_LIVE_FRESH")
	if who == "" {
		t.Skip("set SNAKEMEDIA_LIVE_FRESH to copy an account into a new profile")
	}
	info, err := buildinfo.Parse(manifest)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(info)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	a.ctx = ctx
	a.accounts.Start(ctx, func(events.Event, any) {})
	defer a.shutdown(ctx)

	list := a.accounts.List(accounts.ProviderFlow)
	i := slices.IndexFunc(list.Data, func(acc accounts.Account) bool { return acc.ID == who || acc.Identity == who || acc.Name == who })
	if i < 0 {
		t.Fatalf("no Flow account %s", who)
	}
	src := list.Data[i]
	br, err := a.accounts.Browser(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	policy := a.cfg.Get().Accounts
	targets, err := browser.Targets(ctx, br.Port, policy)
	if err != nil {
		t.Fatal(err)
	}
	k := slices.IndexFunc(targets, func(tg browser.Target) bool { _, ok := tg.Page(); return ok })
	if k < 0 {
		t.Fatal("no page in the account's browser")
	}
	conn, err := browser.Dial(ctx, targets[k], br.Port, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	var all struct {
		Cookies []browser.Cookie `json:"cookies"`
	}
	err = conn.Call(ctx, "Network.getAllCookies", nil, &all)
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(all.Cookies)
	t.Logf("copied %d cookies from %s (%s)", len(all.Cookies), src.Name, src.ID)

	res := a.accounts.ImportCookies(accounts.ProviderFlow, src.Name+" (profile mới)", string(raw))
	if !res.Success {
		t.Fatal(res.Error)
	}
	id := res.Data.ID
	if src.Proxy != "" {
		t.Logf("proxy of the copy: %+v", a.SetAccountProxy(id, src.Proxy))
	}
	for deadline := time.Now().Add(3 * time.Minute); time.Now().Before(deadline); time.Sleep(3 * time.Second) {
		for _, acc := range a.accounts.List(accounts.ProviderFlow).Data {
			if acc.ID == id && acc.Status != accounts.StatusUnverified {
				t.Logf("NEW account %s status=%s identity=%s message=%q", id, acc.Status, acc.Identity, acc.Message)
				return
			}
		}
	}
	t.Fatalf("new account %s was not checked in time", id)
}
