package flow

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"SnakeMedia/internal/jobs"
)

// A job file a newer build saved may be a request Flow already took: it is
// neither read with the wrong meaning, nor overwritten, nor moved aside.
func TestJobFileFromANewerBuildIsLeftAlone(t *testing.T) {
	s := testFlowService(testAccountService(t))
	id := "88888888-8888-4888-8888-888888888888"
	path := filepath.Join(s.stateDir("jobs"), id+".json")
	newer := []byte(`{"formatVersion":99,"id":"` + id + `"}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	var job editJob
	if err := s.readState("jobs", id, &job); !errors.Is(err, jobs.ErrNewerFormat) {
		t.Fatalf("readState = %v, want ErrNewerFormat", err)
	}
	if err := s.saveState("jobs", id, editJob{ID: id}); !errors.Is(err, jobs.ErrNewerFormat) {
		t.Fatalf("saveState = %v, want the write refused", err)
	}
	if _, err := listJournals(s, "jobs", func(string, editJob) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, newer) {
		t.Fatalf("file changed to %s", data)
	}
	if matches, _ := filepath.Glob(path + ".corrupt*"); len(matches) > 0 {
		t.Fatal("a newer job file was moved aside as corrupt")
	}
}

func TestSavedJobFileCarriesTheFormatVersion(t *testing.T) {
	s := testFlowService(testAccountService(t))
	id := "99999999-9999-4999-8999-999999999999"
	if err := s.saveState("jobs", id, editJob{ID: id}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(s.stateDir("jobs"), id+".json"))
	if !bytes.HasPrefix(data, []byte(`{"formatVersion":1,`)) {
		t.Fatalf("saved %s", data)
	}
	var job editJob
	if err := s.readState("jobs", id, &job); err != nil || job.ID != id {
		t.Fatalf("readState = %+v, %v", job, err)
	}
}
