package jobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestJournalReadsTheUnversionedArrayAndWritesTheVersionedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, []byte(`[{"id":"a","tags":[]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	j := New(path, Options[rec]{})
	if err := j.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := j.Get("a"); !ok {
		t.Fatal("record from the unversioned file was lost")
	}
	if err := j.Add(rec{ID: "b"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var file struct {
		FormatVersion int   `json:"formatVersion"`
		Records       []rec `json:"records"`
	}
	if err := json.Unmarshal(data, &file); err != nil || file.FormatVersion != FormatVersion || len(file.Records) != 2 {
		t.Fatalf("saved file = %s (%v)", data, err)
	}
	again := New(path, Options[rec]{})
	if err := again.Load(); err != nil || len(again.List()) != 2 {
		t.Fatalf("reload = %v, %d records", err, len(again.List()))
	}
}

func TestJournalFromANewerBuildIsKeptAndNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	newer := []byte(`{"formatVersion":99,"records":[{"id":"paid"}]}`)
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	j := New(path, Options[rec]{})
	if err := j.Load(); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("Load = %v, want ErrNewerFormat", err)
	}
	if err := j.Add(rec{ID: "x"}); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("Add = %v, want the write refused", err)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, newer) {
		t.Fatalf("file changed to %s", data)
	}
	if matches, _ := filepath.Glob(path + ".corrupt-*"); len(matches) > 0 {
		t.Fatal("a newer file was moved aside as corrupt")
	}
}

func TestStampAndCheckFormat(t *testing.T) {
	for in, want := range map[string]string{
		`{"id":"a"}`: `{"formatVersion":1,"id":"a"}`,
		`{}`:         `{"formatVersion":1}`,
		`[1]`:        `[1]`,
	} {
		if got := string(Stamp([]byte(in))); got != want {
			t.Errorf("Stamp(%s) = %s, want %s", in, got, want)
		}
	}
	if err := CheckFormat(Stamp([]byte(`{"id":"a"}`))); err != nil {
		t.Errorf("own format refused: %v", err)
	}
	if err := CheckFormat([]byte(`{"formatVersion":2}`)); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("newer format = %v", err)
	}
}
