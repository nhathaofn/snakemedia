package muse

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A succeeded job's files reach the library, and clearing the queue or
// removing the job leaves them listed.
func TestLibraryOutlivesTheQueue(t *testing.T) {
	events, files := videoResult("/out/a.mp4", []byte("video-bytes"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"}, events: events, files: files}
	p := testProvider(t, page)
	p.loadLibrary()
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "một con mèo")[0]
	p.runOne(context.Background(), job.ID, acc.ID)

	items := p.ListLibrary().Data
	if len(items) != 1 || items[0].Job != job.ID || items[0].Kind != MuseJobVideo || items[0].Prompt != "một con mèo" {
		t.Fatalf("library = %+v", items)
	}
	if res := p.ClearFinished("", ""); !res.Success || res.Data != 1 {
		t.Fatalf("clear = %+v", res)
	}
	if got := p.ListLibrary().Data; len(got) != 1 {
		t.Fatalf("clearing the queue emptied the library: %+v", got)
	}

	reloaded := newLibrary(p.library.Path())
	if err := reloaded.Load(); err != nil || len(reloaded.List()) != 1 {
		t.Fatalf("library after restart = %v %+v", err, reloaded.List())
	}

	path, _ := p.media.Path(items[0].LocalURL)
	if res := p.RemoveLibraryItems([]string{items[0].ID}); !res.Success || res.Data != 1 {
		t.Fatalf("remove = %+v", res)
	}
	if len(p.ListLibrary().Data) != 0 {
		t.Fatal("removed item still listed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("removing a library item deleted its file: %v", err)
	}
}

// The first start with a library fills it from the succeeded jobs already in
// the queue; later starts never add them again.
func TestLibraryMigratesSucceededJobsOnce(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobImage, "a", "b")
	p.updateJob(jobs[0].ID, func(j *MuseJob) {
		j.Status, j.Files, j.SavedFiles = MuseJobSucceeded, []string{"/flow-media/a1.png", "/flow-media/a2.png"}, []string{"C:/out/a-1.png"}
	})
	p.updateJob(jobs[1].ID, func(j *MuseJob) { j.Status = MuseJobFailed })

	p.loadLibrary()
	items := p.ListLibrary().Data
	if len(items) != 2 {
		t.Fatalf("migrated %+v", items)
	}
	saved := map[string]string{}
	for _, item := range items {
		saved[item.LocalURL] = item.SavedPath
	}
	if saved["/flow-media/a1.png"] != "C:/out/a-1.png" || saved["/flow-media/a2.png"] != "" {
		t.Fatalf("saved paths = %v", saved)
	}

	p.RemoveLibraryItems([]string{items[0].ID})
	p.library = newLibrary(filepath.Join(filepath.Dir(p.jobs.path), "library.json"))
	p.loadLibrary()
	if got := p.ListLibrary().Data; len(got) != 1 {
		t.Fatalf("a later start migrated again: %+v", got)
	}
}

// A job cancelled while its files are being saved keeps the user's decision:
// nothing is copied out to the output folder.
func TestSaveOutputsSkipsCancelledJob(t *testing.T) {
	p := testProvider(t, &fakePage{})
	local, err := p.media.Put("r", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	job := submit(t, p, MuseJobImage, "x")[0]
	p.updateJob(job.ID, func(j *MuseJob) { j.Status, j.OutputDir, j.Index = MuseJobProcessing, p.defaultOutputDir, 1 })
	if saved := p.saveOutputs(mustJob(t, p, job.ID), []string{local}); len(saved) != 1 {
		t.Fatalf("running job saved %v", saved)
	}
	p.updateJob(job.ID, func(j *MuseJob) { j.Status = MuseJobCancelled })
	if saved := p.saveOutputs(mustJob(t, p, job.ID), []string{local}); len(saved) != 0 {
		t.Fatalf("cancelled job saved %v", saved)
	}
}

// A Hàng loạt batch saves each file into the folder it was queued with, named
// by the prompt's place in the batch; Đơn saves nothing (downloaded by hand).
func TestBatchSavesNumberedFilesSingleSavesNothing(t *testing.T) {
	_, files := videoResult("/out/a.mp4", []byte("video"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Videos: []ResultMedia{{Path: "/out/a.mp4", Mime: "video/mp4", ByteLen: 5}}}}, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	dir := resolvedTempDir(t)
	if res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Mode: MuseJobBatch, Prompts: []string{"a", "b"}}); res.Success {
		t.Fatal("a batch without a folder was queued")
	}
	if res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Mode: MuseJobBatch, Prompts: []string{"a"}, OutputDir: dir}); res.Success {
		t.Fatal("a batch was queued into a folder nobody chose")
	}
	p.media.AllowRoot(dir)
	batch := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Mode: MuseJobBatch, Prompts: []string{"a", "b"}, OutputDir: dir})
	single := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Mode: MuseJobSingle, Prompts: []string{"c"}, OutputDir: dir})
	if !batch.Success || !single.Success {
		t.Fatalf("submit: %s %s", batch.Error, single.Error)
	}

	p.runOne(context.Background(), batch.Data[1].ID, acc.ID)
	p.runOne(context.Background(), single.Data[0].ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, batch.Data[1].ID)
	if got.Status != MuseJobSucceeded || len(got.SavedFiles) != 1 || filepath.Dir(got.SavedFiles[0]) != batch.Data[1].OutputDir ||
		filepath.Dir(batch.Data[1].OutputDir) != dir ||
		!regexp.MustCompile(`^2-snakemedia-muse-[a-z0-9]{6}\.mp4$`).MatchString(filepath.Base(got.SavedFiles[0])) {
		t.Fatalf("batch job = %+v", got)
	}
	if s := mustJob(t, p, single.Data[0].ID); s.Status != MuseJobSucceeded || len(s.SavedFiles) != 0 {
		t.Fatalf("single job = %+v", s)
	}
}

// Prompts added in several goes while a batch is unfinished join its folder
// and continue its numbers; once it is done the next prompts open a new folder.
func TestBatchFolderJoinsOpenBatch(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	dir := resolvedTempDir(t)
	p.media.AllowRoot(dir)
	submit := func(kind MuseJobKind, prompts ...string) []MuseJob {
		t.Helper()
		res := p.SubmitJobs(MuseJobRequest{Kind: kind, Mode: MuseJobBatch, Prompts: prompts, OutputDir: dir, Held: true})
		if !res.Success {
			t.Fatalf("submit: %s", res.Error)
		}
		return res.Data
	}
	first := submit(MuseJobImage, "a", "b")
	more := submit(MuseJobImage, "c")
	video := submit(MuseJobVideo, "v")
	if filepath.Dir(first[0].OutputDir) != dir || more[0].OutputDir != first[0].OutputDir {
		t.Fatalf("folders: %q %q", first[0].OutputDir, more[0].OutputDir)
	}
	if first[0].Index != 1 || first[1].Index != 2 || more[0].Index != 3 {
		t.Fatalf("numbers: %d %d %d", first[0].Index, first[1].Index, more[0].Index)
	}
	if video[0].OutputDir == first[0].OutputDir || video[0].Index != 1 {
		t.Fatalf("video batch = %q #%d", video[0].OutputDir, video[0].Index)
	}

	for _, j := range append(first, more...) {
		p.updateJob(j.ID, func(j *MuseJob) { j.Status = MuseJobSucceeded })
	}
	next := submit(MuseJobImage, "d")
	if next[0].OutputDir == first[0].OutputDir || filepath.Dir(next[0].OutputDir) != dir || next[0].Index != 1 {
		t.Fatalf("after the batch ended: %q #%d", next[0].OutputDir, next[0].Index)
	}
}

// resolvedTempDir is t.TempDir in the form saved paths take: links and
// Windows 8.3 names (RUNNER~1 on CI) resolved, as media roots resolve them.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
