package flow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/providers/flow/session"

	"github.com/google/uuid"
)

// TestLiveTrust measures how often Google refuses image generation for
// unusual activity, alternating how the billable RPC leaves the app: from
// Go's HTTP client or from the account's Flow page. It runs the app's own
// flow (headless Chrome per the saved settings) on one real account and
// spends credits.
//
//	SNAKEMEDIA_LIVE_TRUST=<rounds>  SNAKEMEDIA_LIVE_DATA=<data folder>
//	SNAKEMEDIA_LIVE_ACCOUNT=<account id, default: the first valid one>
//	SNAKEMEDIA_LIVE_GAP=<seconds between sends, default 20>
//	SNAKEMEDIA_LIVE_MODES=go,browser (order to alternate)
func TestLiveTrust(t *testing.T) {
	rounds, _ := strconv.Atoi(os.Getenv("SNAKEMEDIA_LIVE_TRUST"))
	if rounds <= 0 || os.Getenv("SNAKEMEDIA_LIVE_DATA") == "" {
		t.Skip("set SNAKEMEDIA_LIVE_TRUST and SNAKEMEDIA_LIVE_DATA to measure refusals (spends credits)")
	}
	realPaths := paths.ForRoot(os.Getenv("SNAKEMEDIA_LIVE_DATA"))
	cfg, err := config.NewManager(realPaths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("headless=%v", cfg.Get().Accounts.Headless)
	accounts := liveAccounts(t, realPaths, cfg)
	s := New(accounts, cfg, media.NewStore(filepath.Join(t.TempDir(), "media")), "SnakeMedia")
	valid, err := s.GetValidConnectedAccounts()
	if err != nil || len(valid) == 0 {
		t.Fatalf("no valid account: %v", err)
	}
	target := valid[0]
	if id := os.Getenv("SNAKEMEDIA_LIVE_ACCOUNT"); id != "" {
		i := slices.IndexFunc(valid, func(a session.StoredAccount) bool { return a.ID == id || a.Email == id })
		if i < 0 {
			connected, _ := accounts.GetConnectedStoredAccounts()
			for _, a := range connected {
				t.Logf("connected %s %s cookies=%d valid=%v", a.ID, a.Email, len(a.Credentials.Cookies), ValidateAccountCookies(a.Credentials.Cookies))
			}
			t.Fatalf("account %s is not valid", id)
		}
		target = valid[i]
	}
	var others []string
	for _, a := range valid {
		t.Logf("account %s %s", a.ID, a.Email)
		if a.ID != target.ID {
			others = append(others, a.ID)
		}
	}
	gap := 20 * time.Second
	if v, _ := strconv.Atoi(os.Getenv("SNAKEMEDIA_LIVE_GAP")); v > 0 {
		gap = time.Duration(v) * time.Second
	}
	modes := []string{"go", "browser"}
	if v := os.Getenv("SNAKEMEDIA_LIVE_MODES"); v != "" {
		modes = strings.Split(v, ",")
	}

	ctx := context.Background()
	models := s.GetImageModels(ctx)
	if !models.Success || len(models.Data) == 0 {
		t.Fatalf("image models: %s", models.Error)
	}
	model := models.Data[0]
	t.Logf("target=%s model=%s gap=%s modes=%v", target.Email, model.ID, gap, modes)

	type tally struct{ ok, refused, other int }
	results := map[string]*tally{}
	for i := range rounds {
		mode := modes[i%len(modes)]
		if results[mode] == nil {
			results[mode] = &tally{}
		}
		s.rpcFromGo = mode == "go"
		res := s.GenerateImages(ctx, GenerateImageRequest{
			RequestID: uuid.NewString(), Prompt: "a small red paper boat on a calm lake, round " + strconv.Itoa(i+1),
			ModelID: model.ID, AspectRatio: "1:1", Count: 1, avoid: others,
		})
		switch {
		case res.Success:
			results[mode].ok++
		case strings.Contains(res.Error, "hoạt động bất thường"):
			results[mode].refused++
		default:
			results[mode].other++
		}
		t.Logf("round %d mode=%s ok=%v err=%q", i+1, mode, res.Success, res.Error)
		if i < rounds-1 {
			time.Sleep(gap)
		}
	}
	for mode, r := range results {
		t.Logf("RESULT mode=%s ok=%d refused=%d other=%d", mode, r.ok, r.refused, r.other)
	}
}
