package media

import (
	"bytes"
	"os"
	"testing"
)

func TestPreserveOriginalKeepsFirstDownloadedBytes(t *testing.T) {
	s := NewStore(t.TempDir())
	raw := []byte("original download")
	url, err := s.Put("video", "video/mp4", ".mp4", raw)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := s.PreserveOriginal(url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("video", "video/mp4", ".mp4", []byte("processed")); err != nil {
		t.Fatal(err)
	}
	if again, err := s.PreserveOriginal(url); err != nil || again != archive {
		t.Fatalf("repeat: %s %v", again, err)
	}
	got, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(raw, got) {
		t.Fatal("original overwritten")
	}
	if _, err := s.Path(Route + "originals/video.mp4"); err == nil {
		t.Fatal("archive exposed by single-file media route")
	}
}

func TestPreserveOriginalRejectsOutsideAndMissingFiles(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, url := range []string{"/other/video.mp4", Route + "../outside.mp4", Route + "missing.mp4"} {
		if _, err := s.PreserveOriginal(url); err == nil {
			t.Fatalf("accepted %s", url)
		}
	}
}
