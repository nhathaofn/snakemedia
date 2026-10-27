package media

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// savedFile is a name OutputName makes, with an extension.
var savedFile = regexp.MustCompile(`^\d+-snakemedia-(flow|muse)-[a-z0-9]{6}\.\w+$`)

func TestOutputName(t *testing.T) {
	a, b := OutputName(3, "muse"), OutputName(3, "muse")
	if !savedFile.MatchString(a+".mp4") || a[:len("3-snakemedia-muse-")] != "3-snakemedia-muse-" {
		t.Fatalf("name %q", a)
	}
	if a == b {
		t.Fatal("two names share their random code")
	}
	if got := OutputName(0, "flow"); got[:2] != "1-" {
		t.Fatalf("a missing number starts at 1: %q", got)
	}
}

func TestNextNumberFollowsSavedFiles(t *testing.T) {
	dir := t.TempDir()
	if n := NextNumber(dir); n != 1 {
		t.Fatalf("empty folder: %d", n)
	}
	for _, name := range []string{"2-snakemedia-flow-abc123.png", "9-snakemedia-muse-zzz999.mp4", "15-other.png", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if n := NextNumber(dir); n != 10 {
		t.Fatalf("next = %d, want 10", n)
	}
}

func TestProviderOf(t *testing.T) {
	if ProviderOf(Route+"muse-job-1.mp4") != "muse" || ProviderOf(Route+"abc.png") != "flow" {
		t.Fatal("provider not read from the stored name")
	}
}
