package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.10.0", "1.9.9", true},
		{"v2.0.0", "1.99.99", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"1.0.0", "1.0.0-beta", true},
		{"1.0.0-beta", "1.0.0", false},
		{"nonsense", "1.0.0", false},
		{"1.0", "0.9.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

type fakeRelease struct {
	tag       string
	installer []byte
	sums      string
}

func serve(t *testing.T, rel *fakeRelease) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/demo/releases/latest":
			name := "Demo-" + strings.TrimPrefix(rel.tag, "v") + "-windows-amd64-setup.exe"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": rel.tag, "body": "notes", "html_url": "https://example.test/rel",
				"assets": []map[string]any{
					{"name": name, "browser_download_url": srv.URL + "/dl/setup", "size": len(rel.installer)},
					{"name": checksumsAsset, "browser_download_url": srv.URL + "/dl/sums", "size": len(rel.sums)},
				},
			})
		case "/dl/setup":
			_, _ = w.Write(rel.installer)
		case "/dl/sums":
			_, _ = w.Write([]byte(rel.sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testUpdater(t *testing.T, srv *httptest.Server, exe string) *Updater {
	u := New("Demo", "1.0.0", "acme/demo", filepath.Join(t.TempDir(), "updates"))
	u.api = srv.URL
	u.executable = func() (string, error) { return exe, nil }
	return u
}

func sumLine(data []byte, name string) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]) + "  " + name + "\n"
}

func TestCheckAndDownloadVerifiesTheInstaller(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("installs are Windows only")
	}
	installer := []byte("MZ fake installer")
	rel := &fakeRelease{tag: "v1.2.0", installer: installer, sums: sumLine(installer, "Demo-1.2.0-windows-amd64-setup.exe")}
	u := testUpdater(t, serve(t, rel), `C:\Apps\Demo\Demo.exe`)

	got, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Latest != "1.2.0" || !got.CanInstall || got.Notes != "notes" {
		t.Fatalf("Check = %+v", got)
	}
	var last Progress
	path, err := u.Download(context.Background(), func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != string(installer) || last.Done != int64(len(installer)) {
		t.Fatalf("downloaded %q, progress %+v", data, last)
	}
	u.Cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Cleanup left the installer")
	}
}

func TestDownloadRefusesAMismatchedChecksum(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("installs are Windows only")
	}
	rel := &fakeRelease{tag: "v1.2.0", installer: []byte("tampered"), sums: sumLine([]byte("original"), "Demo-1.2.0-windows-amd64-setup.exe")}
	u := testUpdater(t, serve(t, rel), `C:\Apps\Demo\Demo.exe`)
	if _, err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Download(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("Download = %v, want a checksum refusal", err)
	}
	if entries, _ := os.ReadDir(u.dir); len(entries) != 0 {
		t.Fatalf("a refused download left %d files", len(entries))
	}
}

func TestCheckSaysWhenThereIsNothingNew(t *testing.T) {
	u := testUpdater(t, serve(t, &fakeRelease{tag: "v1.0.0"}), `C:\Apps\Demo\Demo.exe`)
	got, err := u.Check(context.Background())
	if err != nil || got.Available {
		t.Fatalf("Check = %+v, %v", got, err)
	}
	if _, err := u.Download(context.Background(), nil); err == nil {
		t.Fatal("Download ran with nothing newer")
	}
}

func TestDevAndPortableCopiesAreNotReplaced(t *testing.T) {
	for _, exe := range []string{`C:\src\build\bin\Demo-dev.exe`, `C:\Downloads\Demo-1.0.0-windows-amd64-portable.exe`} {
		u := testUpdater(t, serve(t, &fakeRelease{tag: "v1.2.0"}), exe)
		got, err := u.Check(context.Background())
		if err != nil || got.CanInstall || got.Reason == "" {
			t.Errorf("%s: Check = %+v, %v", exe, got, err)
		}
	}
}

func TestUpdatesOffWithoutARepository(t *testing.T) {
	if _, err := New("Demo", "1.0.0", "", t.TempDir()).Check(context.Background()); err != ErrDisabled {
		t.Fatalf("Check = %v, want ErrDisabled", err)
	}
}
