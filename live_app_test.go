package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/providers/flow"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/muse"
	"github.com/google/uuid"
)

// TestLiveApp drives the real App on the user's own data folder and accounts.
// It spends real Flow credits and Muse quota, so it runs only when
// SNAKEMEDIA_LIVE_APP lists the steps to run (comma separated):
//
//	list         print every account and why it cannot run
//	flow-image   one Flow image, then the same request ID again (must not pay twice)
//	flow-video   one cheapest Flow video, the wait cancelled midway, then resumed
//	muse-image   one Muse image job, waited on the queue
//	muse-video   one Muse video job, waited on the queue
//	muse-probe   read how Muse reports chat progress (sends nothing)
//	muse-watch   send SNAKEMEDIA_LIVE_WATCH prompts ("|" separated) and log every state Muse reports
//	muse-setup   what follows a sign-in: the check, then the setup message, waiting for Muse's reply
//	             (SNAKEMEDIA_LIVE_ACCOUNT picks the account)
//	muse-limits  check every signed-in Muse account and print the attachment limits read (sends nothing)
//
// SNAKEMEDIA_LIVE_DIRECT lists account IDs to switch to "no proxy" first.
func TestLiveApp(t *testing.T) {
	steps := strings.Split(os.Getenv("SNAKEMEDIA_LIVE_APP"), ",")
	if steps[0] == "" {
		t.Skip("set SNAKEMEDIA_LIVE_APP to run against the real accounts (spends credits)")
	}
	info, err := buildinfo.Parse(manifest)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(info)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.ctx = ctx
	noop := func(events.Event, any) {}
	a.accounts.Start(ctx, noop)
	a.providers.Start(ctx, noop)
	defer a.shutdown(ctx)

	for _, id := range strings.Split(os.Getenv("SNAKEMEDIA_LIVE_DIRECT"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			res := a.SetAccountProxy(id, "none")
			t.Logf("direct proxy for %s: ok=%v %s", id, res.Success, res.Error)
		}
	}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			switch strings.TrimSpace(step) {
			case "list":
				liveList(t, a)
			case "rpcs":
				accs, err := a.flow.GetValidConnectedAccounts()
				if err != nil {
					t.Fatal(err)
				}
				for _, acc := range accs {
					t.Logf("%s rpcs=%s build=%s", acc.Email, dump(acc.Credentials.RPCs), acc.Credentials.BuildLabel)
				}
			case "credits":
				accs, err := a.flow.GetValidConnectedAccounts()
				if err != nil {
					t.Fatal(err)
				}
				for _, acc := range accs {
					b, err := a.accounts.Browser(a.ctx, acc.ID)
					if err != nil {
						t.Fatal(err)
					}
					credits, creds, err := chrome.QueryCredits(a.ctx, b.Port, acc.Credentials, a.cfg.Get().FlowPagePolicy())
					for i := 0; i < 15 && errors.Is(err, chrome.ErrPageNotReady); i++ {
						time.Sleep(2 * time.Second)
						credits, creds, err = chrome.QueryCredits(a.ctx, b.Port, acc.Credentials, a.cfg.Get().FlowPagePolicy())
					}
					t.Logf("%s credits=%s rpcs credits=%q availability=%q err=%v", acc.Email, dump(credits), creds.RPCs.Credits, creds.RPCs.Availability, err)
				}
			case "flow-image":
				liveFlowImage(t, a)
			case "flow-video":
				liveFlowVideo(t, a)
			case "muse-image":
				liveMuse(t, a, muse.MuseJobImage)
			case "muse-video":
				liveMuse(t, a, muse.MuseJobVideo)
			case "muse-probe":
				liveMuseProbe(t, a)
			case "muse-watch":
				liveMuseWatch(t, a)
			case "muse-setup":
				liveMuseSetup(t, a)
			case "muse-limits":
				liveMuseLimits(t, a)
			default:
				t.Fatalf("unknown step %q", step)
			}
		})
	}
}

func dump(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func liveList(t *testing.T, a *App) {
	res := a.accounts.List("")
	if !res.Success {
		t.Fatal(res.Error)
	}
	for _, acc := range res.Data {
		t.Logf("%s %s %q enabled=%v status=%s proxy=%q noRoute=%q usage=%s message=%q",
			acc.Provider, acc.ID, acc.Name, acc.Enabled, acc.Status, acc.Proxy, acc.NoRoute, dump(acc.Usage), acc.Message)
	}
}

func liveFlowImage(t *testing.T, a *App) {
	ms := a.GetImageModels()
	if !ms.Success {
		t.Fatal(ms.Error)
	}
	t.Logf("image models: %s", dump(ms.Data))
	model := ms.Data[0]
	if want := os.Getenv("SNAKEMEDIA_LIVE_IMAGE_MODEL"); want != "" {
		i := slices.IndexFunc(ms.Data, func(m flow.FlowModel) bool { return m.ID == want })
		if i < 0 {
			t.Fatalf("no image model %s", want)
		}
		model = ms.Data[i]
	}
	req := flow.GenerateImageRequest{
		RequestID: uuid.NewString(), Prompt: "a small red paper boat on a calm lake, morning light",
		ModelID: model.ID, AspectRatio: "1:1", Count: 1,
	}
	t.Logf("image request %s model=%s", req.RequestID, model.ID)
	first := a.GenerateImages(req)
	t.Logf("first: ok=%v code=%v err=%q items=%s", first.Success, first.Code, first.Error, dump(first.Data))
	if !first.Success {
		t.FailNow()
	}
	// The same request ID again must be answered from the journal, not sent.
	again := a.GenerateImages(req)
	t.Logf("again: ok=%v err=%q items=%s", again.Success, again.Error, dump(again.Data))
	resumed := a.ResumeImageGeneration(req.RequestID)
	t.Logf("resume: ok=%v err=%q items=%s", resumed.Success, resumed.Error, dump(resumed.Data))
}

func liveFlowVideo(t *testing.T, a *App) {
	ms := a.GetVideoModels()
	if !ms.Success {
		t.Fatal(ms.Error)
	}
	t.Logf("video models: %s", dump(ms.Data))
	// The cheapest priced variant.
	var req flow.GenerateVideoRequest
	best := -1
	for _, m := range ms.Data {
		for _, c := range m.Costs {
			if c.Credits > 0 && (best < 0 || c.Credits < best) {
				best = c.Credits
				dur := c.Duration
				if dur == 0 && len(m.Durations) > 0 {
					dur = slices.Min(m.Durations)
				}
				req = flow.GenerateVideoRequest{ModelID: m.ID, Duration: dur, Resolution: c.Resolution}
				if len(m.AspectRatios) > 0 {
					req.AspectRatio = m.AspectRatios[0]
				}
			}
		}
	}
	if best < 0 {
		t.Fatal("no priced video variant")
	}
	req.RequestID, req.Prompt, req.Count = uuid.NewString(), "a red paper boat drifting slowly on a calm lake", 1
	t.Logf("video request %s %s credits=%d", req.RequestID, dump(req), best)

	done := make(chan any, 1)
	go func() { done <- a.GenerateVideos(req) }()
	time.Sleep(12 * time.Second)
	t.Logf("cancel wait: %v", a.CancelGeneration(req.RequestID).Data)
	first := (<-done)
	t.Logf("after cancel: %s", dump(first))

	resumed := a.ResumeVideoGeneration(req.RequestID)
	t.Logf("resume: ok=%v err=%q items=%s", resumed.Success, resumed.Error, dump(resumed.Data))
	for i := 0; i < 20 && resumed.Success && len(resumed.Data) > 0 && resumed.Data[0].Pending; i++ {
		time.Sleep(15 * time.Second)
		resumed = a.ResumeVideoGeneration(req.RequestID)
		t.Logf("resume again: ok=%v err=%q items=%s", resumed.Success, resumed.Error, dump(resumed.Data))
	}
}

func liveMuse(t *testing.T, a *App, kind muse.MuseJobKind) {
	opts := a.GetMuseOptions()
	t.Logf("muse options: ok=%v err=%q %s", opts.Success, opts.Error, dump(opts.Data))
	prompt := "a small red paper boat on a calm lake, morning light"
	if kind == muse.MuseJobVideo {
		prompt = "a red paper boat drifting slowly on a calm lake, 5 seconds"
	}
	// SNAKEMEDIA_LIVE_MUSE_PROMPT, _RATIO, _DURATION and _REFS (image paths,
	// "|" separated, attached in that order) shape the job.
	if env := os.Getenv("SNAKEMEDIA_LIVE_MUSE_PROMPT"); env != "" {
		prompt = env
	}
	req := muse.MuseJobRequest{Kind: kind, Mode: muse.MuseJobSingle, Prompts: []string{prompt}, Repeat: 1, Label: "live test",
		Ratio: os.Getenv("SNAKEMEDIA_LIVE_MUSE_RATIO")}
	req.Duration, _ = strconv.Atoi(os.Getenv("SNAKEMEDIA_LIVE_MUSE_DURATION"))
	// SNAKEMEDIA_LIVE_MUSE_MODE=batch queues it as Hàng loạt, saved into a
	// batch folder under the default Muse output folder.
	if os.Getenv("SNAKEMEDIA_LIVE_MUSE_MODE") == "batch" {
		req.Mode, req.OutputDir = muse.MuseJobBatch, a.batchOutputDefaults[accounts.ProviderMuse]
	}
	if env := os.Getenv("SNAKEMEDIA_LIVE_MUSE_REFS"); env != "" {
		refs := a.muse.ImportReferences(strings.Split(env, "|"))
		if !refs.Success {
			t.Fatalf("references: %s", refs.Error)
		}
		for _, r := range refs.Data {
			req.References = append(req.References, r.LocalURL)
		}
	}
	sub := a.SubmitMuseJobs(req)
	if !sub.Success || len(sub.Data) != 1 {
		t.Fatalf("submit: %s %s", sub.Error, dump(sub.Data))
	}
	id := sub.Data[0].ID
	t.Logf("muse job %s queued", id)
	last := ""
	deadline := time.Now().Add(25 * time.Minute)
	for time.Now().Before(deadline) {
		list := a.ListMuseJobs()
		i := slices.IndexFunc(list.Data, func(j muse.MuseJob) bool { return j.ID == id })
		if i < 0 {
			t.Fatal("job vanished")
		}
		j := list.Data[i]
		state := string(j.Status) + " | " + j.Step + " | " + j.Error
		if state != last {
			t.Logf("%s account=%s session=%s sending=%v attempts=%d files=%v",
				state, j.AccountID, j.SessionID, j.Sending, j.Attempts, j.Files)
			last = state
		}
		if j.Status == muse.MuseJobSucceeded || j.Status == muse.MuseJobFailed || j.Status == muse.MuseJobCancelled {
			t.Logf("final: %s", dump(j))
			for _, f := range j.Files {
				if path, err := a.media.Path(f); err == nil {
					t.Logf("file: %s", path)
				}
			}
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Logf("still running after 25 minutes; it stays on the queue")
}
