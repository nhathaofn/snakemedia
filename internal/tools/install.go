package tools

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/pkg/utils"
)

const (
	receiptFile = "tool.json"
	// maxMetadataBytes bounds a version or checksum file.
	maxMetadataBytes = 4 << 10
	// progressEvery spaces progress events of a download.
	progressEvery = 250 * time.Millisecond
)

var versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,63}$`)

// receipt is written next to an installed tool's programs.
type receipt struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installedAt"`
}

func readReceipt(dir string) (receipt, error) {
	data, err := os.ReadFile(filepath.Join(dir, receiptFile))
	if err != nil {
		return receipt{}, err
	}
	var r receipt
	return r, json.Unmarshal(data, &r)
}

// Install downloads a tool into the app's tools folder, checks its SHA-256 and
// replaces the previous copy only once the new one is complete. Progress is
// sent as events.EventToolProgress.
func (s *Service) Install(ctx context.Context, id string) (Tool, error) {
	spec, ok := s.spec(id)
	if !ok {
		return Tool{}, fmt.Errorf("Không có công cụ %q.", id)
	}
	src, ok := spec.source()
	if !ok {
		return Tool{}, fmt.Errorf("%s không có bản tải cho máy này; hãy cài thủ công.", spec.Name)
	}
	s.mu.Lock()
	if s.installing[id] {
		s.mu.Unlock()
		return Tool{}, fmt.Errorf("%s đang được tải.", spec.Name)
	}
	s.installing[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.installing, id)
		delete(s.progress, id)
		s.mu.Unlock()
	}()

	if err := s.install(ctx, spec, src); err != nil {
		logger.Get().Warn("tool install failed", "tool", id, "error", err)
		return Tool{}, err
	}
	logger.Get().Info("tool installed", "tool", id)
	s.mu.Lock()
	delete(s.waiting, id)
	delete(s.postponed, id)
	s.mu.Unlock()
	return s.status(spec), nil
}

func (s *Service) install(ctx context.Context, spec Spec, src Source) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	version := ""
	if src.VersionURL != "" {
		text, err := s.small(ctx, src.VersionURL)
		if err != nil {
			return err
		}
		if version = strings.TrimSpace(text); !versionPattern.MatchString(version) {
			return fmt.Errorf("Phiên bản %s đọc được không hợp lệ.", spec.Name)
		}
	}
	url := strings.ReplaceAll(src.URL, "{version}", version)
	want := src.SHA256
	if want == "" {
		shaURL := strings.NewReplacer("{url}", url, "{version}", version).Replace(src.SHA256URL)
		text, err := s.small(ctx, shaURL)
		if err != nil {
			return err
		}
		if fields := strings.Fields(text); len(fields) > 0 {
			want = strings.ToLower(fields[0])
		}
		if !sha256Pattern.MatchString(want) {
			return fmt.Errorf("Không đọc được mã SHA-256 của %s.", spec.Name)
		}
	}

	archive, err := os.CreateTemp(s.dir, ".download-"+spec.ID+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	err = s.download(ctx, spec.ID, url, src.MaxBytes, want, archive)
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	stage, err := os.MkdirTemp(s.dir, ".stage-"+spec.ID+"-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := extract(archive.Name(), stage, spec.Programs); err != nil {
		return fmt.Errorf("Không giải nén được %s: %w", spec.Name, err)
	}
	data, _ := json.MarshalIndent(receipt{Version: version, URL: url, SHA256: want, InstalledAt: time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	if err := utils.WriteFileAtomic(filepath.Join(stage, receiptFile), data, 0o644); err != nil {
		return err
	}
	target := s.toolDir(spec.ID)
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("Không thay được bản %s cũ (có thể đang được dùng): %w", spec.Name, err)
	}
	return os.Rename(stage, target)
}

func (s *Service) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Không tải được công cụ: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("Tải công cụ lỗi HTTP %d.", resp.StatusCode)
	}
	return resp, nil
}

// small reads a version or checksum file.
func (s *Service) small(ctx context.Context, url string) (string, error) {
	resp, err := s.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes))
	return string(data), err
}

// report records and sends a download's progress.
func (s *Service) report(p Progress) {
	s.mu.Lock()
	s.progress[p.Tool] = p
	s.mu.Unlock()
	s.send(events.EventToolProgress, p)
}

// download writes url to out, at most limit bytes, and checks its SHA-256.
func (s *Service) download(ctx context.Context, id, url string, limit int64, want string, out io.Writer) error {
	resp, err := s.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	total := resp.ContentLength
	if total > limit {
		return fmt.Errorf("Tệp tải về lớn hơn giới hạn %d byte.", limit)
	}
	hash := sha256.New()
	var done int64
	last := time.Time{}
	buf := make([]byte, 256<<10)
	body := io.LimitReader(resp.Body, limit+1)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			hash.Write(buf[:n])
			done += int64(n)
			if time.Since(last) >= progressEvery {
				last = time.Now()
				s.report(Progress{Tool: id, Done: done, Total: total})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("Tải công cụ bị ngắt: %w", readErr)
		}
	}
	s.report(Progress{Tool: id, Done: done, Total: done})
	if done > limit || (total > 0 && done != total) {
		return fmt.Errorf("Tệp tải về không đủ hoặc quá lớn (%d byte).", done)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return errors.New("Tệp tải về không khớp mã SHA-256 của nhà phát hành; đã huỷ.")
	}
	return nil
}

// extract copies the listed programs out of a zip, wherever they sit in it,
// into dir as <program><exe suffix>.
func extract(archive, dir string, programs []string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	wanted := map[string]string{}
	for _, p := range programs {
		wanted[strings.ToLower(p+exeSuffix())] = p + exeSuffix()
	}
	found := map[string]bool{}
	for _, f := range r.File {
		name, ok := wanted[strings.ToLower(path.Base(f.Name))]
		if !ok || found[name] || f.FileInfo().IsDir() {
			continue
		}
		if err := extractFile(f, filepath.Join(dir, name)); err != nil {
			return err
		}
		found[name] = true
	}
	for _, name := range wanted {
		if !found[name] {
			return fmt.Errorf("không có %s trong gói", name)
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, int64(f.UncompressedSize64)+1)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
