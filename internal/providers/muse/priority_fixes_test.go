package muse

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
)

func TestSubmissionIDConcurrentAndAfterReload(t *testing.T) {
	p := testProvider(t, &fakePage{})
	req := MuseJobRequest{SubmissionID: "one-click", Kind: MuseJobImage, Mode: MuseJobSingle, Prompts: []string{"a", "b"}, Repeat: 2}
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := p.SubmitJobs(req)
			if !res.Success || len(res.Data) != 4 {
				t.Errorf("submit: %+v", res)
				return
			}
			ids <- res.Data[0].ID
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if first != id {
			t.Fatal("duplicate submission made another job")
		}
	}
	if len(p.jobs.list()) != 4 {
		t.Fatal("duplicate jobs")
	}
	p.jobs = newJobStore(p.jobs.path, func() int { return 500 })
	if err := p.jobs.load(); err != nil {
		t.Fatal(err)
	}
	if res := p.SubmitJobs(req); !res.Success || res.Data[0].ID != first {
		t.Fatalf("reloaded: %+v", res)
	}
	req.Prompts = []string{"changed"}
	if res := p.SubmitJobs(req); res.Success {
		t.Fatal("same ID accepted a different payload")
	}
	req.SubmissionID = "another-click"
	if res := p.SubmitJobs(req); !res.Success || res.Data[0].ID == first {
		t.Fatalf("new action: %+v", res)
	}
}

func TestOutputFailureResumesAfterReloadWithoutGeneration(t *testing.T) {
	page := &fakePage{}
	p := testProvider(t, page)
	first, err := p.media.Put("muse-a", "video/mp4", ".mp4", []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, _ := p.media.Put("muse-b", "video/mp4", ".mp4", []byte("second"))
	secondPath, _ := p.media.Path(second)
	if err := os.Remove(secondPath); err != nil {
		t.Fatal(err)
	}
	job := submit(t, p, MuseJobVideo, "a")[0]
	job, _ = p.updateJob(job.ID, func(j *MuseJob) {
		j.Status = MuseJobProcessing
		j.Files = []string{first, second}
		j.OutputDir = p.defaultOutputDir
		j.Index = 1
	})
	job, err = p.saveCollected(job, job.Files)
	if err != nil || job.SaveError == "" || len(job.SavedFiles) != 1 {
		t.Fatalf("partial: %+v, %v", job, err)
	}
	firstCopy := job.OutputPaths[first]
	job, _ = p.updateJob(job.ID, func(j *MuseJob) { j.Status = MuseJobSucceeded })
	p.addToLibrary(job)
	for _, item := range p.ListLibrary().Data {
		if item.LocalURL == second && item.SavedPath != "" {
			t.Fatal("failed copy assigned another file's path")
		}
	}
	p.jobs = newJobStore(p.jobs.path, func() int { return 500 })
	if err := p.jobs.load(); err != nil {
		t.Fatal(err)
	}
	if got := mustJob(t, p, job.ID); got.SaveError == "" {
		t.Fatal("save error lost on restart")
	}
	_, _ = p.media.Put("muse-b", "video/mp4", ".mp4", []byte("second"))
	res := p.RetrySave(job.ID)
	if !res.Success || res.Data.SaveError != "" || len(res.Data.SavedFiles) != 2 || res.Data.OutputPaths[first] != firstCopy {
		t.Fatalf("retry: %+v", res)
	}
	if again := p.RetrySave(job.ID); !again.Success || !reflect.DeepEqual(again.Data.SavedFiles, res.Data.SavedFiles) {
		t.Fatalf("second retry: %+v", again)
	}
	entries, _ := os.ReadDir(job.OutputDir)
	if len(entries) != 2 || len(page.sends) != 0 {
		t.Fatalf("copies %d, sends %d", len(entries), len(page.sends))
	}
	for _, item := range p.ListLibrary().Data {
		if item.SavedPath == "" {
			t.Fatal("library path not updated")
		}
	}
}

func TestBatchSaveFailureDoesNotDiscardGeneratedResult(t *testing.T) {
	events, files := videoResult("/out.mp4", []byte("video"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "chat"}, events: events, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "a")[0]
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	p.media.AllowRoot(blocker)
	p.updateJob(job.ID, func(j *MuseJob) { j.OutputDir = filepath.Join(blocker, "batch") })
	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SaveError == "" || len(got.Files) != 1 || len(page.sends) != 1 {
		t.Fatalf("job: %+v", got)
	}
}

// mp4WithDuration contains actual ISO BMFF metadata. Tests use the production
// header reader rather than stubbing the measured duration; no codec is needed.
func mp4WithDuration(seconds uint32) []byte {
	box := func(kind string, body []byte) []byte {
		out := make([]byte, len(body)+8)
		binary.BigEndian.PutUint32(out, uint32(len(out)))
		copy(out[4:], kind)
		copy(out[8:], body)
		return out
	}
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	binary.BigEndian.PutUint32(mvhd[16:], seconds*1000)
	tkhd := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhd[40:], 1<<16)
	binary.BigEndian.PutUint32(tkhd[56:], 1<<16)
	binary.BigEndian.PutUint32(tkhd[76:], 720<<16)
	binary.BigEndian.PutUint32(tkhd[80:], 1280<<16)
	hdlr := make([]byte, 24)
	copy(hdlr[8:], "vide")
	trak := box("trak", append(box("tkhd", tkhd), box("mdia", box("hdlr", hdlr))...))
	return append(box("ftyp", []byte("isom0000")), box("moov", append(box("mvhd", mvhd), trak...))...)
}

func TestLongVideoValidatesDoneAgainstFile(t *testing.T) {
	for _, seconds := range []uint32{10, 60, 80} {
		t.Run(time.Duration(seconds).String(), func(t *testing.T) {
			data := mp4WithDuration(seconds)
			events, files := videoResult("/out.mp4", data)
			events[0].Text = "STATUS: DONE"
			page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "chat"}, events: events, files: files}
			p := testProvider(t, page)
			acc := addSignedIn(t, p, "A")
			job := submitLong(t, p, 60)
			p.runOne(context.Background(), job.ID, acc.ID)
			p.wg.Wait()
			got := mustJob(t, p, job.ID)
			if seconds == 60 {
				if got.Status != MuseJobSucceeded || got.FormatNote != "" {
					t.Fatalf("exact: %+v", got)
				}
			} else if got.Status != MuseJobFailed || !strings.Contains(got.Error, "thời lượng") || len(got.Files) != 1 || len(p.ListLibrary().Data) != 1 {
				t.Fatalf("incomplete: %+v", got)
			}
			if len(page.sends) != 1 || len(page.sendTos) != 0 {
				t.Fatal("validation sent another generation")
			}
		})
	}
}

func TestLongVideoWithoutStatusWaitsForJoinedFile(t *testing.T) {
	events, files := videoResult("/out.mp4", mp4WithDuration(10))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "chat"}, events: events, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 60)
	p.runOne(context.Background(), job.ID, acc.ID)
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobProcessing || got.Rechecks != 1 || len(got.Files) != 1 {
		t.Fatalf("waiting: %+v", got)
	}
	page.mu.Lock()
	page.files["/out.mp4"] = mp4WithDuration(60)
	page.events[0].Videos[0].ByteLen = int64(len(page.files["/out.mp4"]))
	page.mu.Unlock()
	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()
	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || len(page.sends) != 1 {
		t.Fatalf("joined: %+v", got)
	}
}

func TestFairQueueAcrossPlanningRounds(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := MuseAccount{ID: "a", Enabled: true, Status: accounts.StatusSignedIn}
	jobs := []MuseJob{
		{ID: "b1", Status: MuseJobQueued, Mode: MuseJobBatch, CreatedAt: "1"},
		{ID: "b2", Status: MuseJobQueued, Mode: MuseJobBatch, CreatedAt: "2"},
		{ID: "s1", Status: MuseJobQueued, Mode: MuseJobSingle, CreatedAt: "3"},
		{ID: "s2", Status: MuseJobQueued, Mode: MuseJobSingle, CreatedAt: "4"},
	}
	last := MuseJobMode("")
	var order []string
	for range 4 {
		got := plan(jobs, []MuseAccount{acc}, config.DefaultMuseConfig(), map[string]string{}, p.now(), last)
		if len(got) != 1 {
			t.Fatalf("plan: %+v", got)
		}
		order = append(order, got[0].jobID)
		for i := range jobs {
			if jobs[i].ID == got[0].jobID {
				jobs[i].Status = MuseJobSucceeded
				last = jobs[i].Mode
			}
		}
	}
	if !reflect.DeepEqual(order, []string{"b1", "s1", "b2", "s2"}) {
		t.Fatalf("order %v", order)
	}
}

type countingDownloadPage struct {
	fakePage
	downloads int
}

func (p *countingDownloadPage) Download(ctx context.Context, path string, sink io.Writer) (int64, error) {
	p.downloads++
	return p.fakePage.Download(ctx, path, sink)
}

func TestDownloadAlwaysEnforcesConfiguredCap(t *testing.T) {
	page := &countingDownloadPage{fakePage: fakePage{files: map[string][]byte{"/out": []byte("small")}}}
	p := testProvider(t, page)
	cfg := p.cfg.Get()
	cfg.Accounts.MaxDownloadBytes = 1 << 20
	if _, err := p.cfg.Update(cfg); err != nil {
		t.Fatal(err)
	}
	cap := p.cfg.Get().Accounts.MaxDownloadBytes
	if _, err := p.download(context.Background(), page, ResultMedia{Path: "/out", Mime: "video/mp4", ByteLen: cap + 1}, "large"); err == nil || page.downloads != 0 {
		t.Fatal("oversized metadata started a download")
	}
	if _, err := p.download(context.Background(), page, ResultMedia{Path: "/out", Mime: "video/mp4", ByteLen: 3}, "wrong-size"); err == nil {
		t.Fatal("actual bytes exceeded declared size")
	}
	if _, err := p.download(context.Background(), page, ResultMedia{Path: "/out", Mime: "video/mp4"}, "unknown-size"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(p.media.Dir())
	if len(entries) != 1 {
		t.Fatalf("failed download left files: %v", entries)
	}
	page.files["/out"] = bytes.Repeat([]byte("x"), int(cap)+1)
	if _, err := p.download(context.Background(), page, ResultMedia{Path: "/out", Mime: "video/mp4"}, "oversized-body"); err == nil {
		t.Fatal("unknown size bypassed configured cap")
	}
	entries, _ = os.ReadDir(p.media.Dir())
	if len(entries) != 1 {
		t.Fatal("oversized body left a partial file")
	}
}

func TestSubmissionRetryPersistsAfterDiskFailure(t *testing.T) {
	p := testProvider(t, &fakePage{})
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	p.jobs = newJobStore(filepath.Join(blocker, "jobs.json"), func() int { return 500 })
	req := MuseJobRequest{SubmissionID: "retry-write", Kind: MuseJobImage, Prompts: []string{"a"}}
	if res := p.SubmitJobs(req); res.Success {
		t.Fatal("acknowledged a failed write")
	}
	first := p.jobs.list()[0].ID
	if res := p.SubmitJobs(req); res.Success {
		t.Fatal("acknowledged memory-only duplicate")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if res := p.SubmitJobs(req); !res.Success || res.Data[0].ID != first {
		t.Fatalf("retry: %+v", res)
	}
	store := newJobStore(p.jobs.path, func() int { return 500 })
	if err := store.load(); err != nil {
		t.Fatal(err)
	}
	if len(store.list()) != 1 || store.list()[0].ID != first {
		t.Fatal("retry did not persist exactly one job")
	}
}

func TestDownloadsSerializeOnlyWithinTheirAccount(t *testing.T) {
	p := testProvider(t, &fakePage{})
	release, err := p.downloadSlot(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.downloadSlot(ctx, "a"); err == nil {
		t.Fatal("same account opened a second download")
	}
	other, err := p.downloadSlot(context.Background(), "b")
	if err != nil {
		t.Fatal(err)
	}
	other()
	release()
	next, err := p.downloadSlot(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestLongVideoUnmeasurableKeepsChatAndFile(t *testing.T) {
	events, files := videoResult("/out.mp4", []byte("invalid MP4"))
	events[0].Text = "STATUS: DONE"
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "chat"}, events: events, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 60)
	p.runOne(context.Background(), job.ID, acc.ID)
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobProcessing || got.Rechecks != 1 || got.SessionID != "chat" || len(got.Files) != 1 || len(page.sends) != 1 {
		t.Fatalf("unverified: %+v", got)
	}
}
