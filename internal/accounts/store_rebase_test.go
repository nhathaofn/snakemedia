package accounts

import (
	"path/filepath"
	"testing"
)

func TestRebaseProfiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dev", "accounts", "profiles")
	inside := filepath.Join(root, "a")
	other := filepath.Join(t.TempDir(), "release", "accounts", "profiles", "b")
	recs := []Record{{ProfileDir: inside}, {ProfileDir: other}, {}}
	rebaseProfiles(recs, root)
	if recs[0].ProfileDir != inside {
		t.Errorf("inside path changed: %s", recs[0].ProfileDir)
	}
	if want := filepath.Join(root, "b"); recs[1].ProfileDir != want {
		t.Errorf("outside path = %s, want %s", recs[1].ProfileDir, want)
	}
	if recs[2].ProfileDir != "" {
		t.Errorf("empty path changed: %s", recs[2].ProfileDir)
	}
}
