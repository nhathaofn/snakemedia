package muse

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/engine"
)

func TestNodeHandsOnMediaRefs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobVideo, "a cat")
	if _, err := p.jobs.update(jobs[0].ID, func(j *MuseJob) {
		j.Status, j.Files = MuseJobSucceeded, []string{"/flow-media/out-1"}
	}); err != nil {
		t.Fatal(err)
	}
	res, err := p.awaitJobs(context.Background(), MuseJobVideo, jobs, false)
	if err != nil || len(res.Files) != 1 || res.Files[0].Kind != engine.MediaVideo || res.Files[0].LocalURL != "/flow-media/out-1" {
		t.Fatalf("files handed on as media references: %+v %v", res, err)
	}
}

func TestStoppedNodeCancelsOnlyUnsentJobs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobImage, "sent", "waiting")
	if _, err := p.jobs.update(jobs[0].ID, func(j *MuseJob) {
		j.Status, j.SessionID = MuseJobProcessing, "chat-1"
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.awaitJobs(ctx, MuseJobImage, jobs, true)
	var pending *engine.PendingError
	if !errors.As(err, &pending) || len(pending.JobIDs) != 1 || pending.JobIDs[0] != jobs[0].ID {
		t.Fatalf("the sent job must keep the node pending: %v", err)
	}
	if mustJob(t, p, jobs[0].ID).Status != MuseJobProcessing {
		t.Fatal("a job Muse already took must keep running")
	}
	if mustJob(t, p, jobs[1].ID).Status != MuseJobCancelled {
		t.Fatal("an unsent job must be cancelled when asked")
	}
}

func TestStoppedNodeKeepsQueueWithoutCancelOnStop(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobImage, "waiting")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.awaitJobs(ctx, MuseJobImage, jobs, false)
	var pending *engine.PendingError
	if !errors.As(err, &pending) || mustJob(t, p, jobs[0].ID).Status != MuseJobQueued {
		t.Fatalf("without cancelOnStop the job stays queued: %v", err)
	}
}

// TestGraphRerunFindsItsJobs submits the same graph run twice: the second
// submit must return the jobs of the first instead of queueing new ones, and
// only a job Muse never took moves on to a new attempt. Also verifies changing
// ratio, duration, assets, or storyboard parameters creates fresh jobs.
func TestGraphRerunFindsItsJobs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	req := MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"a", "b"}, Ratio: "9:16", Duration: 10}
	first := mustSubmit(t, p, req, "run-1/m")
	again := mustSubmit(t, p, req, "run-1/m")
	if len(again) != 2 || again[0].ID != first[0].ID || again[1].ID != first[1].ID || len(p.jobs.list()) != 2 {
		t.Fatalf("a re-run must find its jobs: %+v vs %+v", again, first)
	}
	changed := req
	changed.Ratio = "16:9"
	diffRatio := mustSubmit(t, p, changed, "run-1/m")
	if diffRatio[0].ID == first[0].ID {
		t.Fatalf("changing ratio should queue new run job, got same ID %s", diffRatio[0].ID)
	}
	changedDur := req
	changedDur.Duration = 20
	diffDur := mustSubmit(t, p, changedDur, "run-1/m")
	if diffDur[0].ID == first[0].ID {
		t.Fatalf("changing duration should queue new run job, got same ID %s", diffDur[0].ID)
	}
	if _, err := p.jobs.update(first[0].ID, func(j *MuseJob) { j.Status = MuseJobFailed }); err != nil {
		t.Fatal(err)
	}
	if _, err := p.jobs.update(first[1].ID, func(j *MuseJob) { j.Status, j.SessionID = MuseJobFailed, "chat-1" }); err != nil {
		t.Fatal(err)
	}
	retry := mustSubmit(t, p, req, "run-1/m")
	if retry[0].ID == first[0].ID || retry[1].ID != first[1].ID {
		t.Fatalf("only the never-sent failure is queued again: %+v", retry)
	}
	if other := mustSubmit(t, p, req, ""); other[0].ID == first[0].ID {
		t.Fatal("without a run key every job is new")
	}
}

func TestGraphRerunKeepsJobWithUnknownSend(t *testing.T) {
	p := testProvider(t, &fakePage{})
	req := MuseJobRequest{Kind: MuseJobImage, Prompts: []string{"one image"}}
	const key = "run-unknown/image"
	job := mustSubmit(t, p, req, key)[0]
	if _, err := p.jobs.update(job.ID, func(j *MuseJob) {
		j.Status, j.SendUnknown, j.Error = MuseJobFailed, true, errSendUnknown.Error()
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.jobs.load(); err != nil {
		t.Fatal(err)
	}
	again := mustSubmit(t, p, req, key)
	if len(again) != 1 || again[0].ID != job.ID || len(p.jobs.list()) != 1 {
		t.Fatalf("rerun created a job after an unknown send: %+v", again)
	}
	if err := p.jobs.remove(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.jobs.load(); err != nil {
		t.Fatal(err)
	}
	if res := p.submitJobs(req, key); res.Success || res.Error != errGraphJobRetired.Error() || len(p.jobs.list()) != 0 {
		t.Fatalf("rerun created a job after a billable record was retired: %+v", res)
	}
}

func TestNodeOptionSourcesAndParamsCheck(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)
	saveAsset(t, p, "lan", "Lan", MuseRoleFace, "")

	reg := engine.NewRegistry()
	if err := reg.Register(p.Nodes()...); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterOptions(p.OptionSources()); err != nil {
		t.Fatal(err)
	}
	if err := reg.Check(); err != nil {
		t.Fatalf("engine registry option check failed: %v", err)
	}
	opts, err := reg.Options(context.Background(), OptionsVideoDurations, nil)
	if err != nil || len(opts) < 2 || opts[0].Value != "0" || opts[1].Value != "10" {
		t.Fatalf("video duration options = %+v, err=%v", opts, err)
	}
	imgRatios, err := reg.Options(context.Background(), OptionsImageRatios, nil)
	if err != nil || len(imgRatios) < 2 || imgRatios[0].Value != "" {
		t.Fatalf("image ratio options = %+v, err=%v", imgRatios, err)
	}
	assetsOpts, err := reg.Options(context.Background(), OptionsAssets, nil)
	if err != nil || len(assetsOpts) != 1 || !strings.Contains(assetsOpts[0].Label, "@lan") {
		t.Fatalf("asset options = %+v, err=%v", assetsOpts, err)
	}
}

func TestVideoNodeSupportsRatioDurationAssetsAndStoryboardEndToEnd(t *testing.T) {
	sbBytes := testPNGBytes(t, 16, 16)
	page := &fakePage{
		loggedIn: true,
		sendRes:  SendResult{OK: true, SessionID: "s1"},
		statuses: []string{"completed"},
		events: []AssistantEvent{{Images: []ResultMedia{
			{Path: "/sb-1.png", Mime: "image/png", ByteLen: int64(len(sbBytes))},
			{Path: "/sb-2.png", Mime: "image/png", ByteLen: int64(len(sbBytes))},
		}}},
		files: map[string][]byte{"/sb-1.png": sbBytes, "/sb-2.png": sbBytes},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)
	saveAsset(t, p, "lan", "Lan", MuseRoleFace, "left cheek mole")

	reg := engine.NewRegistry()
	if err := reg.Register(p.Nodes()...); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterOptions(p.OptionSources()); err != nil {
		t.Fatal(err)
	}

	params, _ := json.Marshal(map[string]any{
		"prompt":           "cô gái bước dưới mưa",
		"ratio":            "9:16",
		"duration":         "10",
		"assets":           "@lan",
		"storyboardFrames": 2,
	})
	g := engine.Graph{
		RequestID: "graph-1",
		Nodes:     []engine.GraphNode{{ID: "vid", Type: NodeVideoGenerate, Params: params}},
	}

	// Run node in background and drive its storyboard + video queue steps on testProvider.
	type outRes struct {
		gr  engine.GraphResult
		err error
	}
	done := make(chan outRes, 1)
	go func() {
		gr, err := reg.RunGraph(context.Background(), g)
		done <- outRes{gr, err}
	}()

	// Wait until submitJobs queues the storyboard image job + video job.
	var boardJob, videoJob MuseJob
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		list := p.jobs.list()
		if len(list) == 2 {
			boardJob, videoJob = list[0], list[1]
			break
		}
		select {
		case r := <-done:
			t.Fatalf("RunGraph ended early: %+v %v", r.gr, r.err)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if boardJob.Kind != MuseJobImage || boardJob.BoardFrames != 2 || videoJob.Kind != MuseJobVideo || videoJob.Ratio != "9:16" || videoJob.Duration != 10 {
		t.Fatalf("unexpected jobs created: board=%+v video=%+v", boardJob, videoJob)
	}
	if len(videoJob.RefInfo) != 1 || videoJob.RefInfo[0].Tag != "lan" {
		t.Fatalf("expected resolved @lan ref on video job, got %+v", videoJob.RefInfo)
	}

	// Run storyboard image job, release waiting video job, then deliver the video file.
	p.runOne(context.Background(), boardJob.ID, acc.ID)
	p.releaseWaiting()
	page.events, page.files = videoResult("/final.mp4", []byte("vid-bytes"))
	page.statuses = []string{"completed"}
	p.runOne(context.Background(), videoJob.ID, acc.ID)

	res := <-done
	if res.err != nil || res.gr.Nodes["vid"].Status != engine.RunDone {
		t.Fatalf("RunGraph failed: %+v err=%v", res.gr, res.err)
	}
	var nodeOut NodeResult
	if err := json.Unmarshal(res.gr.Nodes["vid"].Output, &nodeOut); err != nil {
		t.Fatal(err)
	}
	// PortVideo output MUST contain ONLY the final video, NEVER intermediate storyboard PNGs.
	if len(nodeOut.Files) != 1 || nodeOut.Files[0].Kind != engine.MediaVideo {
		t.Fatalf("expected only 1 video MediaRef in output, got %+v", nodeOut.Files)
	}
	if !strings.Contains(page.sends[1], "Aspect ratio: 9:16 (portrait)") ||
		!strings.Contains(page.sends[1], "Duration: 10 seconds") ||
		!strings.Contains(page.sends[1], "Storyboard: image 1 to image 2") ||
		!strings.Contains(page.sends[1], "image 3 = @lan") {
		t.Fatalf("video message missing expected node settings:\n%s", page.sends[1])
	}
}

func TestVideoNodeStoryboardFailureDoesNotHang(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)

	jobs := mustSubmit(t, p, MuseJobRequest{
		Kind: MuseJobVideo, Prompts: []string{"scene"}, Storyboard: &MuseStoryboard{Frames: 2},
	}, "")
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if _, err := p.jobs.update(jobs[0].ID, func(j *MuseJob) {
		j.Status, j.Error = MuseJobFailed, "board refused"
	}); err != nil {
		t.Fatal(err)
	}
	_, err := p.awaitJobs(context.Background(), MuseJobVideo, jobs, false)
	if err == nil || !strings.Contains(err.Error(), "board refused") {
		t.Fatalf("expected storyboard error to fail awaitJobs cleanly, got %v", err)
	}
}

func mustSubmit(t *testing.T, p *Provider, req MuseJobRequest, runKey string) []MuseJob {
	t.Helper()
	res := p.submitJobs(req, runKey)
	if !res.Success {
		t.Fatalf("submit failed: %s", res.Error)
	}
	return res.Data
}
