package muse

import (
	"errors"
	"path/filepath"
	"testing"
)

// A run that panicked while its message was on the way must not count as
// "not sent": Muse may have taken it, so it fails as errSendUnknown.
func TestPanicWhileSendingIsAnUnknownSend(t *testing.T) {
	s := newJobStore(filepath.Join(t.TempDir(), "jobs.json"), func() int { return 10 })
	if err := s.add(MuseJob{ID: "sending", Status: MuseJobProcessing, Sending: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.add(MuseJob{ID: "idle", Status: MuseJobProcessing}); err != nil {
		t.Fatal(err)
	}
	crash := errors.New("panic")
	if got := panicFailure(s, "sending", crash); !errors.Is(got, errSendUnknown) {
		t.Fatalf("panic while sending = %v, want errSendUnknown", got)
	}
	if got := panicFailure(s, "idle", crash); got != crash {
		t.Fatalf("panic before sending = %v, want the panic error", got)
	}
}
