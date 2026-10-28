// Package updater finds a newer release of the app on GitHub, downloads its
// installer, checks it against the release's SHA256SUMS.txt and runs it
// silently over the current install once the app has closed.
//
// The release layout is made by .github/workflows/release.yml: a tag vX.Y.Z,
// the asset <name>-X.Y.Z-windows-amd64-setup.exe and SHA256SUMS.txt.
package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// githubAPI is GitHub's REST API; tests point an Updater elsewhere.
	githubAPI = "https://api.github.com"
	// checksumsAsset lists "<sha256>  <file>" for every asset of a release.
	checksumsAsset = "SHA256SUMS.txt"
	// maxInstallerBytes bounds the download (the installer is a few tens of MB).
	maxInstallerBytes = 512 << 20
	// maxMetadataBytes bounds the release JSON and the checksum list.
	maxMetadataBytes = 1 << 20
	// checkTimeout bounds one release lookup.
	checkTimeout = 30 * time.Second
)

// ErrDisabled means the build names no repository to update from.
var ErrDisabled = errors.New("Bản build này không có nguồn cập nhật (wails.json update.githubRepository).")

// Release is what a check found, for the settings screen.
type Release struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	Notes       string `json:"notes"`
	PageURL     string `json:"pageUrl"`
	PublishedAt string `json:"publishedAt"`
	// CanInstall is false for a copy the updater must not replace (a dev
	// build, the portable exe, another OS); Reason says why.
	CanInstall bool   `json:"canInstall"`
	Reason     string `json:"reason,omitempty"`
}

// Progress reports a download.
type Progress struct {
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type githubRelease struct {
	Tag         string        `json:"tag_name"`
	Body        string        `json:"body"`
	PageURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []githubAsset `json:"assets"`
}

// Updater checks and installs releases of one app.
type Updater struct {
	name, repo, current string
	dir                 string
	api                 string
	client              *http.Client
	executable          func() (string, error)

	mu     sync.Mutex
	latest *githubRelease
}

// New returns an updater for app name at version current, reading releases of
// repo ("owner/name", empty for none) and downloading into dir.
func New(name, current, repo, dir string) *Updater {
	return &Updater{
		name: name, current: current, repo: repo, dir: dir,
		api: githubAPI, client: &http.Client{}, executable: os.Executable,
	}
}

// Check reads the latest release.
func (u *Updater) Check(ctx context.Context) (Release, error) {
	if u.repo == "" {
		return Release{}, ErrDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.api+"/repos/"+u.repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", u.name+"/"+u.current)
	resp, err := u.client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("Không kết nối được máy chủ cập nhật: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, errors.New("Chưa có bản phát hành nào (hoặc repo cập nhật là private).")
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("Máy chủ cập nhật trả lỗi HTTP %d.", resp.StatusCode)
	}
	var rel githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("Không đọc được thông tin bản phát hành: %w", err)
	}
	latest := strings.TrimPrefix(rel.Tag, "v")
	if _, err := parseVersion(latest); err != nil || rel.Draft || rel.Prerelease {
		return Release{}, fmt.Errorf("Bản phát hành mới nhất %q không phải bản chính thức.", rel.Tag)
	}
	u.mu.Lock()
	u.latest = &rel
	u.mu.Unlock()

	out := Release{
		Current: u.current, Latest: latest, Available: Newer(latest, u.current),
		Notes: rel.Body, PageURL: rel.PageURL, PublishedAt: rel.PublishedAt,
	}
	out.CanInstall, out.Reason = u.canInstall()
	if out.Available && out.CanInstall {
		if _, ok := findAsset(rel.Assets, u.installerName(latest)); !ok {
			out.CanInstall, out.Reason = false, "Bản phát hành thiếu bộ cài "+u.installerName(latest)+"."
		}
	}
	return out, nil
}

// Download fetches the installer of the release the last Check found and
// returns its path once its SHA256 matches the release's checksum list.
func (u *Updater) Download(ctx context.Context, progress func(Progress)) (string, error) {
	u.mu.Lock()
	rel := u.latest
	u.mu.Unlock()
	if rel == nil {
		return "", errors.New("Hãy kiểm tra cập nhật trước khi tải.")
	}
	version := strings.TrimPrefix(rel.Tag, "v")
	if !Newer(version, u.current) {
		return "", errors.New("Đang dùng bản mới nhất.")
	}
	if ok, reason := u.canInstall(); !ok {
		return "", errors.New(reason)
	}
	name := u.installerName(version)
	asset, ok := findAsset(rel.Assets, name)
	if !ok {
		return "", fmt.Errorf("Bản phát hành thiếu bộ cài %s.", name)
	}
	sums, ok := findAsset(rel.Assets, checksumsAsset)
	if !ok {
		return "", fmt.Errorf("Bản phát hành thiếu %s, không kiểm tra được bộ cài.", checksumsAsset)
	}
	want, err := u.checksum(ctx, sums.URL, name)
	if err != nil {
		return "", err
	}
	if asset.Size <= 0 || asset.Size > maxInstallerBytes {
		return "", fmt.Errorf("Kích thước bộ cài không hợp lệ (%d byte).", asset.Size)
	}
	if err := os.MkdirAll(u.dir, 0o700); err != nil {
		return "", err
	}
	final := filepath.Join(u.dir, name)
	part := final + ".part"
	if err := u.fetch(ctx, asset.URL, part, asset.Size, want, progress); err != nil {
		_ = os.Remove(part)
		return "", err
	}
	if err := os.Rename(part, final); err != nil {
		_ = os.Remove(part)
		return "", err
	}
	return final, nil
}

// Cleanup removes downloads left by an earlier update; run it at startup.
func (u *Updater) Cleanup() {
	entries, err := os.ReadDir(u.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			_ = os.Remove(filepath.Join(u.dir, e.Name()))
		}
	}
}

func (u *Updater) installerName(version string) string {
	return fmt.Sprintf("%s-%s-windows-amd64-setup.exe", u.name, version)
}

// canInstall reports whether the running copy is one the installer replaces:
// the installed <name>.exe on Windows. A dev build (<name>-dev.exe) or the
// portable exe is updated by hand.
func (u *Updater) canInstall() (bool, string) {
	if runtime.GOOS != "windows" {
		return false, "Tự cập nhật chỉ hỗ trợ Windows."
	}
	exe, err := u.executable()
	if err != nil {
		return false, "Không xác định được vị trí app: " + err.Error()
	}
	if !strings.EqualFold(filepath.Base(exe), u.name+".exe") {
		return false, "Bản đang chạy không phải bản cài đặt (" + filepath.Base(exe) + "); hãy tải bộ cài mới ở trang phát hành."
	}
	return true, ""
}

func (u *Updater) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", u.name+"/"+u.current)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Không tải được bản cập nhật: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("Tải bản cập nhật lỗi HTTP %d.", resp.StatusCode)
	}
	return resp, nil
}

// checksum returns the SHA256 the release lists for file.
func (u *Updater) checksum(ctx context.Context, url, file string) (string, error) {
	resp, err := u.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxMetadataBytes))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == file && len(fields[0]) == sha256.Size*2 {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("%s không có dòng cho %s.", checksumsAsset, file)
}

// fetch writes url to path, at most size bytes, and checks its SHA256.
func (u *Updater) fetch(ctx context.Context, url, path string, size int64, want string, progress func(Progress)) error {
	resp, err := u.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	var done int64
	buf := make([]byte, 256<<10)
	body := io.LimitReader(resp.Body, size+1)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			hash.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(Progress{Done: done, Total: size})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			return fmt.Errorf("Tải bản cập nhật bị ngắt: %w", readErr)
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if done != size {
		return fmt.Errorf("Bộ cài tải về không đủ (%d/%d byte).", done, size)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return errors.New("Bộ cài tải về không khớp mã SHA256 của bản phát hành; đã huỷ.")
	}
	return nil
}

func findAsset(assets []githubAsset, name string) (githubAsset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return githubAsset{}, false
}

// version is X.Y.Z with an optional -prerelease.
type version struct {
	nums [3]int
	pre  string
}

func parseVersion(s string) (version, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("version %q is not X.Y.Z", s)
	}
	var v version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, fmt.Errorf("version %q is not X.Y.Z", s)
		}
		v.nums[i] = n
	}
	v.pre = pre
	return v, nil
}

// Newer reports whether version a is later than b. An unparsable version is
// never newer, so a broken tag cannot trigger an install.
func Newer(a, b string) bool {
	va, errA := parseVersion(a)
	vb, errB := parseVersion(b)
	if errA != nil || errB != nil {
		return false
	}
	for i := range va.nums {
		if va.nums[i] != vb.nums[i] {
			return va.nums[i] > vb.nums[i]
		}
	}
	// 1.2.0 is later than 1.2.0-beta; two pre-releases compare as text.
	switch {
	case va.pre == vb.pre:
		return false
	case va.pre == "":
		return true
	case vb.pre == "":
		return false
	}
	return va.pre > vb.pre
}
