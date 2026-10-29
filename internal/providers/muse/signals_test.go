package muse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/media"
	"github.com/google/uuid"
)

func TestSignalLogOncePerValue(t *testing.T) {
	set := onceSet{seen: map[string]bool{}}
	if !set.first("unknown:a=1") || set.first("unknown:a=1") || !set.first("unknown:a=2") {
		t.Fatal("each distinct signal must be logged exactly once")
	}
}

func TestSessionStatusLoggedOnce(t *testing.T) {
	status := "status-" + uuid.NewString()
	if !noteStatus(status) {
		t.Fatal("a new status must be logged")
	}
	if noteStatus(status) {
		t.Fatal("a status already seen must not be logged again")
	}
}

func TestPollKeepsWaitingOnUnknownStatusAndLogsIt(t *testing.T) {
	unknown := "warming-" + uuid.NewString()
	page := &fakePage{loggedIn: true, statuses: []string{unknown, "completed"}}
	p := testProvider(t, page)
	job := MuseJob{ID: "j", Kind: MuseJobImage, SessionID: "s", SentAt: p.now().UTC().Format(time.RFC3339)}
	if ended, err := p.poll(t.Context(), job, page, p.cfg.Get().Muse); err != nil || ended != sessionCompleted {
		t.Fatalf("poll: %v", err)
	}
	if statusLog.first(unknown) {
		t.Fatal("poll should have logged the unknown status")
	}
}

func TestSweepReferencesKeepsUsedAndYoungFiles(t *testing.T) {
	p := testProvider(t, &fakePage{})
	dir := p.media.Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := p.now().Add(-referenceRetention - time.Hour)
	write := func(name string, at time.Time) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write(referencePrefix+"used.png", old)
	write(referencePrefix+"young.png", p.now())
	write(referencePrefix+"old.png", old)
	write("muse-job-result.png", old)    // a result, not a reference
	write("flow-ref-something.png", old) // another provider's file
	if _, err := p.jobs.add(MuseJob{ID: "j1", Kind: MuseJobImage, Status: MuseJobFailed, Prompt: "x",
		References: []string{media.Route + referencePrefix + "used.png"}}); err != nil {
		t.Fatal(err)
	}
	if n := p.sweepReferences(); n != 1 {
		t.Fatalf("removed %d files, want 1", n)
	}
	for name, want := range map[string]bool{
		referencePrefix + "used.png": true, referencePrefix + "young.png": true, referencePrefix + "old.png": false,
		"muse-job-result.png": true, "flow-ref-something.png": true,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if exists := err == nil; exists != want {
			t.Errorf("%s exists=%v, want %v", name, exists, want)
		}
	}
}

func TestSubmitNamesMissingReferences(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "a")
	p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) {
		d.Capabilities = &MuseCapabilities{MaxFileBytes: 1 << 20, MaxFiles: 4, CheckedAt: p.now().UTC().Format(time.RFC3339),
			ImageTypes: []MuseFileType{{Mime: "image/png", Extensions: []string{".png"}}}}
	})
	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Prompts: []string{"a"},
		References: []string{media.Route + referencePrefix + "gone.png"}})
	if res.Success || !strings.Contains(res.Error, referencePrefix+"gone.png") {
		t.Fatalf("missing reference not named: %+v", res)
	}
}
