package muse

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/media"
	"github.com/google/uuid"
)

func TestStructuredSignalsWinOverWording(t *testing.T) {
	var (
		policy *policyError
		usage  *usageError
		verify *verifyError
		muse   *museError
	)
	cases := []struct {
		name    string
		events  []AssistantEvent
		check   func(error) bool
		wantMsg string
	}{
		{"refusal flag", []AssistantEvent{{Text: "Xin lỗi.", Signals: map[string]string{"payload.refused": "true"}}},
			func(err error) bool { return errors.As(err, &policy) }, "Xin lỗi."},
		{"content filter finish", []AssistantEvent{{Signals: map[string]string{"finish_reason": "content_filter"}}},
			func(err error) bool { return errors.As(err, &policy) }, "finish_reason=content_filter"},
		{"rate limit code", []AssistantEvent{{Text: "Thử lại sau nhé", Signals: map[string]string{"payload.error.code": "RATE_LIMITED"}}},
			func(err error) bool { return errors.As(err, &usage) }, ""},
		{"unauthorized", []AssistantEvent{{Signals: map[string]string{"error": "unauthorized"}}},
			func(err error) bool { return errors.As(err, &verify) }, ""},
		{"failed status", []AssistantEvent{{Text: "Có lỗi", Signals: map[string]string{"status": "failed"}}},
			func(err error) bool { return errors.As(err, &muse) }, ""},
		// The wording says "policy", the structure says usage: structure wins.
		{"structure over wording", []AssistantEvent{{Text: "Yêu cầu này vi phạm chính sách nội dung.", Signals: map[string]string{"status": "quota_exceeded"}}},
			func(err error) bool { return errors.As(err, &usage) }, ""},
		// Several signals: a refusal is never hidden by a technical error.
		{"policy outranks error", []AssistantEvent{{Signals: map[string]string{"status": "error", "blocked": "true"}}},
			func(err error) bool { return errors.As(err, &policy) }, ""},
	}
	for _, c := range cases {
		err := classifyEvents(c.events, "ảnh")
		if !c.check(err) {
			t.Errorf("%s: got %T %v", c.name, err, err)
		}
		if c.wantMsg != "" && !strings.Contains(err.Error(), c.wantMsg) {
			t.Errorf("%s: message %q lacks %q", c.name, err.Error(), c.wantMsg)
		}
	}
}

func TestNeutralOrUnknownSignalsFallBackToWording(t *testing.T) {
	events := []AssistantEvent{{
		Text:    "Yêu cầu này vi phạm chính sách nội dung.",
		Signals: map[string]string{"status": "completed", "blocked": "false", "code": "200"},
	}}
	var policy *policyError
	if err := classifyEvents(events, "ảnh"); !errors.As(err, &policy) {
		t.Fatalf("wording fallback lost: %T %v", err, err)
	}
	class, unknown := signalClass(events[0].Signals)
	if class != "" || len(unknown) != 1 || unknown[0] != "code=200" {
		t.Fatalf("class %q unknown %v", class, unknown)
	}
}

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
	page := &fakePage{loggedIn: true, statuses: []string{unknown, "idle"}}
	p := testProvider(t, page)
	job := MuseJob{ID: "j", Kind: MuseJobImage, SessionID: "s", SentAt: p.now().UTC().Format(time.RFC3339)}
	if err := p.poll(t.Context(), job, page, p.cfg.Get().Muse); err != nil {
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
	if err := p.jobs.add(MuseJob{ID: "j1", Kind: MuseJobImage, Status: MuseJobFailed, Prompt: "x",
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
