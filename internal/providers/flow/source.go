package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"SnakeMedia/internal/media"
	"SnakeMedia/internal/providers/flow/session"
	"SnakeMedia/pkg/utils"
	"github.com/google/uuid"
)

// sourceRecord never accepts ownership or dimensions supplied by the frontend.
// The opaque ID survives restarts and binds the source to its Flow account.
type sourceRecord struct {
	ID         string              `json:"id"`
	AccountID  string              `json:"accountId"`
	ProjectID  string              `json:"projectId"`
	MediaID    string              `json:"mediaId"`
	WorkflowID string              `json:"workflowId"`
	LocalURL   string              `json:"localUrl"`
	Video      UploadedSourceVideo `json:"video"`
	Original   media.VideoMetadata `json:"original"`
}

// stateDir is the folder holding the saved records of one kind.
func (s *Provider) stateDir(kind string) string {
	return filepath.Join(filepath.Dir(s.cfg.Path()), "flow", kind)
}

func (s *Provider) statePath(kind, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("Mã tham chiếu không hợp lệ.")
	}
	return filepath.Join(s.stateDir(kind), id+".json"), nil
}

// stateEntry is one saved record and when it was last written.
type stateEntry struct {
	id       string
	modified time.Time
}

// stateEntries lists the saved records of one kind; foreign files are ignored.
func (s *Provider) stateEntries(kind string) ([]stateEntry, error) {
	entries, err := os.ReadDir(s.stateDir(kind))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]stateEntry, 0, len(entries))
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if entry.IsDir() || !ok || uuid.Validate(id) != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, stateEntry{id: id, modified: info.ModTime()})
	}
	return out, nil
}

func (s *Provider) removeState(kind, id string) error {
	path, err := s.statePath(kind, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// jobRetention is how long finished journals are kept and unconfirmed ones followed.
func (s *Provider) jobRetention() time.Duration {
	return time.Duration(s.cfg.Get().Flow.JobRetentionHours) * time.Hour
}

func (s *Provider) saveState(kind, id string, value any) error {
	path, err := s.statePath(kind, id)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(path, data, 0600)
}

func (s *Provider) readState(kind, id string, out any) error {
	path, err := s.statePath(kind, id)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Không đọc được tham chiếu đã lưu: %w", err)
	}
	return json.Unmarshal(data, out)
}

func (s *Provider) readyOwnedAccount(ctx context.Context, id string) (*session.StoredAccount, func(), error) {
	accounts, err := s.GetValidConnectedAccounts()
	if err != nil {
		return nil, nil, err
	}
	for _, acc := range accounts {
		if acc.ID != id {
			continue
		}
		lease, err := s.acquireSpecificAccount(ctx, acc)
		if err != nil {
			return nil, nil, err
		}
		releaseProfile := s.accounts.AcquireProfile(acc.ProfileDir)
		fresh, err := s.syncSession(ctx, acc)
		if err != nil {
			releaseProfile()
			lease.release()
			return nil, nil, err
		}
		return &fresh, func() {
			releaseProfile()
			lease.release()
		}, nil
	}
	return nil, nil, errors.New("Tài khoản sở hữu nguồn/job không còn kết nối. Hãy kết nối lại tài khoản đó.")
}

func (s *Provider) loadSource(req EditVideoRequest) (*sourceRecord, error) {
	if req.SourceID == "" {
		return nil, nil
	}
	var record sourceRecord
	if err := s.readState("sources", req.SourceID, &record); err != nil {
		return nil, err
	}
	if record.ID != req.SourceID || record.AccountID == "" || record.ProjectID == "" || record.MediaID == "" || record.Video.DurationSeconds <= 0 {
		return nil, errors.New("Thông tin nguồn đã lưu không hợp lệ; hãy tải lại video.")
	}
	if req.SourceURL != "" && req.SourceURL != record.LocalURL {
		return nil, errors.New("Nguồn upload không khớp video đã chọn.")
	}
	return &record, nil
}
