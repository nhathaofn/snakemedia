package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"SnakeMedia/internal/events"
)

// Program names no machine has, so PATH never answers.
const (
	progA = "snakemedia-test-tool-a"
	progB = "snakemedia-test-tool-b"
)

func TestEmbeddedManifestIsValid(t *testing.T) {
	if _, err := ParseManifest(manifestJSON); err != nil {
		t.Fatal(err)
	}
}

func TestParseManifestRejectsUnsafeSources(t *testing.T) {
	for name, src := range map[string]string{
		"http url":    `{"url":"http://x/a.zip","sha256Url":"{url}.sha256","archive":"zip","maxBytes":1}`,
		"no checksum": `{"url":"https://x/a.zip","archive":"zip","maxBytes":1}`,
		"bad archive": `{"url":"https://x/a.zip","sha256Url":"{url}.sha256","archive":"rar","maxBytes":1}`,
	} {
		data := `{"tools":[{"id":"t","name":"T","programs":["t"],"sources":{"windows/amd64":` + src + `}}]}`
		if _, err := ParseManifest([]byte(data)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	dup := `{"tools":[{"id":"t","name":"T","programs":["p"]},{"id":"u","name":"U","programs":["p"]}]}`
	if _, err := ParseManifest([]byte(dup)); err == nil {
		t.Error("a program listed by two tools was accepted")
	}
}

func testManifest(src Source) Manifest {
	return Manifest{Tools: []Spec{{
		ID: "test", Name: "Test", Programs: []string{progA, progB},
		Sources: map[string]Source{runtime.GOOS + "/" + runtime.GOARCH: src},
	}}}
}

func writeProgram(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+exeSuffix())
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProgramAsksForTheToolOnlyWhenFoundNowhere(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{}
	s := newService(testManifest(Source{}), dir, "test", func() map[string]string { return paths })
	var asked []Request
	s.Start(func(e events.Event, data any) {
		if e == events.EventToolMissing {
			asked = append(asked, data.(Request))
		}
	})

	if _, err := s.Program("Tính năng", progA); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing program: err = %v", err)
	}
	if len(asked) != 1 || asked[0].Feature != "Tính năng" || asked[0].Tool.ID != "test" || asked[0].Tool.Ready {
		t.Fatalf("ask = %+v", asked)
	}

	own := writeProgram(t, filepath.Join(dir, "test"), progA)
	if got, err := s.Program("Tính năng", progA); err != nil || got != own {
		t.Fatalf("app copy: %q, %v", got, err)
	}

	// A path the user set wins, and points at its sibling too.
	custom := writeProgram(t, filepath.Join(t.TempDir(), "bin"), progA)
	sibling := writeProgram(t, filepath.Dir(custom), progB)
	paths[progA] = custom
	if got, _ := s.Program("Tính năng", progA); got != custom {
		t.Errorf("configured path ignored: %q", got)
	}
	if got, _ := s.Program("Tính năng", progB); got != sibling {
		t.Errorf("sibling of configured path ignored: %q", got)
	}
	if len(asked) != 1 {
		t.Errorf("found programs asked again: %d asks", len(asked))
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func serve(t *testing.T, archive []byte, checksum string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_, _ = w.Write([]byte("1.2.3\n"))
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = w.Write([]byte(checksum + "  pkg.zip\n"))
		case r.URL.Path == "/pkg-1.2.3.zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallChecksTheArchiveAndKeepsOnlyItsPrograms(t *testing.T) {
	archive := zipOf(t, map[string]string{
		"pkg/bin/" + progA + exeSuffix(): "a",
		"pkg/bin/" + progB + exeSuffix(): "b",
		"pkg/doc/readme.txt":             "x",
	})
	sum := sha256.Sum256(archive)
	srv := serve(t, archive, hex.EncodeToString(sum[:]))
	dir := t.TempDir()
	s := newService(testManifest(Source{VersionURL: srv.URL + "/version", URL: srv.URL + "/pkg-{version}.zip", SHA256URL: "{url}.sha256", Archive: "zip", MaxBytes: 1 << 20}), dir, "test", nil)
	s.client = srv.Client()

	tool, err := s.Install(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if !tool.Ready || tool.Version != "1.2.3" {
		t.Fatalf("installed tool = %+v", tool)
	}
	for _, p := range tool.Programs {
		if p.Origin != OriginApp {
			t.Errorf("%s found at %s (%s), want the app copy", p.Name, p.Path, p.Origin)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "test", "readme.txt")); err == nil {
		t.Error("a file that is not a program was extracted")
	}
	if err := s.Remove("test"); err != nil || s.List()[0].Ready {
		t.Errorf("remove: %v, ready after = %v", err, s.List()[0].Ready)
	}
}

func TestInstallRefusesAChecksumMismatchAndKeepsTheOldCopy(t *testing.T) {
	archive := zipOf(t, map[string]string{progA + exeSuffix(): "a", progB + exeSuffix(): "b"})
	srv := serve(t, archive, strings.Repeat("0", 64))
	dir := t.TempDir()
	old := writeProgram(t, filepath.Join(dir, "test"), progA)
	s := newService(testManifest(Source{VersionURL: srv.URL + "/version", URL: srv.URL + "/pkg-{version}.zip", SHA256URL: "{url}.sha256", Archive: "zip", MaxBytes: 1 << 20}), dir, "test", nil)
	s.client = srv.Client()

	if _, err := s.Install(context.Background(), "test"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("mismatch: err = %v", err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("old copy lost after a failed install: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func TestPostponedToolIsNotAskedAgainThisRun(t *testing.T) {
	s := newService(testManifest(Source{}), t.TempDir(), "test", nil)
	asks := 0
	s.Start(func(e events.Event, _ any) {
		if e == events.EventToolMissing {
			asks++
		}
	})
	_, _ = s.Program("A", progA)
	if len(s.Requests()) != 1 {
		t.Fatalf("unanswered asks = %+v", s.Requests())
	}
	s.Postpone("test")
	_, _ = s.Program("A", progA)
	_, _ = s.Program("B", progB)
	if asks != 1 || len(s.Requests()) != 0 {
		t.Errorf("asks = %d, waiting = %+v after postponing", asks, s.Requests())
	}
}
