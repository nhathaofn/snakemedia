package muse

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
)

// TestLiveAnonymousPage opens muse.ai in a real headless Chrome with a fresh
// temporary profile and checks the page script runs there. It signs in to
// nothing and sends nothing. Run with SNAKEMEDIA_TEST_MUSE=1.
func TestLiveAnonymousPage(t *testing.T) {
	if os.Getenv("SNAKEMEDIA_TEST_MUSE") != "1" {
		t.Skip("set SNAKEMEDIA_TEST_MUSE=1 to open muse.ai in a real Chrome")
	}
	dir := t.TempDir()
	cfg, _ := config.NewManager(filepath.Join(dir, "config.json"))
	accts := accounts.New(filepath.Join(dir, "accounts"), cfg, NewSite(cfg))
	defer accts.Close()
	p := New(dir, filepath.Join(dir, "out"), cfg, media.NewStore(filepath.Join(dir, "media")), accts)
	acc := accounts.Record{
		Account:    accounts.Account{ID: "live", Provider: accounts.ProviderMuse, Name: "live", Enabled: true},
		ProfileDir: filepath.Join(dir, "accounts", "profiles", "live"),
	}
	if err := accts.Seed(acc); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	page, err := p.openPage(ctx, acc)
	if err != nil {
		t.Fatal(err)
	}
	var state LoginState
	for {
		if state, err = page.Login(ctx); err == nil && state.HTTP != 0 {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("page never answered: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("anonymous muse.ai session: %+v", state)
	if state.LoggedIn {
		t.Fatal("a fresh profile cannot be signed in")
	}
}
