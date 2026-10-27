package muse

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A succeeded job's files reach the library, and clearing the queue or
// removing the job leaves them listed.
func TestLibraryOutlivesTheQueue(t *testing.T) {
	events, files := videoResult("/out/a.mp4", []byte("video-bytes"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "idle"}, events: events, files: files}
	p := testProvider(t, page)
	p.loadLibrary()
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "một con mèo")[0]
	p.runOne(context.Background(), job.ID, acc.ID)

	items := p.ListLibrary().Data
	if len(items) != 1 || items[0].Job != job.ID || items[0].Kind != MuseJobVideo || items[0].Prompt != "một con mèo" {
		t.Fatalf("library = %+v", items)
	}
	if res := p.ClearFinished(""); !res.Success || res.Data != 1 {
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
	c := p.cfg.Get()
	c.Muse.AutoSave = true
	if _, err := p.cfg.Update(c); err != nil {
		t.Fatal(err)
	}
	local, err := p.media.Put("r", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	job := submit(t, p, MuseJobImage, "x")[0]
	p.updateJob(job.ID, func(j *MuseJob) { j.Status = MuseJobProcessing })
	if saved := p.saveOutputs(mustJob(t, p, job.ID), []string{local}); len(saved) != 1 {
		t.Fatalf("running job saved %v", saved)
	}
	p.updateJob(job.ID, func(j *MuseJob) { j.Status = MuseJobCancelled })
	if saved := p.saveOutputs(mustJob(t, p, job.ID), []string{local}); len(saved) != 0 {
		t.Fatalf("cancelled job saved %v", saved)
	}
}
