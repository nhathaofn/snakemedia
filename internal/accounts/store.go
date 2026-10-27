package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"SnakeMedia/internal/secure"
	"SnakeMedia/pkg/utils"
)

// legacyStoreHeader marks the first format: every record, cookies included,
// in accounts.dat. It is read once and rewritten in the split format on the
// next change.
const legacyStoreHeader = "SMAC\x02"

// storeHeader marks the split format: accounts.dat keeps only the order of
// account IDs and each record (cookies and provider data included) has its own
// encrypted file under records/, so changing one account rewrites one file.
const storeHeader = "SMAC\x03"

var errNotFound = errors.New("Tài khoản không tồn tại.")

// recordName keeps record file names to plain IDs.
var recordName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// store persists accounts encrypted. A corrupt file is moved aside and blocks
// writes for the run instead of being read as empty.
type store struct {
	mu      sync.Mutex
	path    string
	records []Record
	loaded  bool
	// legacy is set while the records came from the old single-file format;
	// the next save writes every record file, then the new index.
	legacy bool
	err    error
}

func newStore(path string) *store { return &store{path: path} }

func (s *store) recordDir() string { return filepath.Join(filepath.Dir(s.path), "records") }

func (s *store) recordPath(id string) (string, error) {
	if !recordName.MatchString(id) {
		return "", fmt.Errorf("Mã tài khoản không hợp lệ: %q", id)
	}
	return filepath.Join(s.recordDir(), id+".dat"), nil
}

func (s *store) loadLocked() error {
	if s.err != nil || s.loaded {
		return s.err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	var records []Record
	legacy := false
	switch {
	case bytes.HasPrefix(data, []byte(storeHeader)):
		records, err = s.readSplit(data[len(storeHeader):])
	case bytes.HasPrefix(data, []byte(legacyStoreHeader)):
		legacy = true
		err = readProtected(data[len(legacyStoreHeader):], &records)
	default:
		err = errors.New("missing header")
	}
	if err != nil {
		backup := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().Format("20060102-150405"))
		if moveErr := os.Rename(s.path, backup); moveErr != nil {
			s.err = fmt.Errorf("Dữ liệu tài khoản hỏng và không cách ly được: %w", moveErr)
		} else if errors.Is(err, secure.ErrOtherUser) {
			s.err = fmt.Errorf("Không đọc được danh sách tài khoản. %s Bản cũ được giữ tại %s; mở lại app để bắt đầu danh sách mới rồi đăng nhập lại hoặc dán lại cookie: %w", secure.ForeignDataHelp, backup, err)
		} else {
			s.err = fmt.Errorf("Dữ liệu tài khoản hỏng (%v) đã được giữ tại %s. Khôi phục dữ liệu trước khi tiếp tục.", err, backup)
		}
		return s.err
	}
	s.records, s.legacy, s.loaded = records, legacy, true
	return nil
}

// readSplit reads the ID order and then each account's record file.
func (s *store) readSplit(index []byte) ([]Record, error) {
	var ids []string
	if err := readProtected(index, &ids); err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(ids))
	for _, id := range ids {
		path, err := s.recordPath(id)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("tài khoản %s: %w", id, err)
		}
		var rec Record
		if !bytes.HasPrefix(data, []byte(storeHeader)) {
			err = errors.New("missing header")
		} else {
			err = readProtected(data[len(storeHeader):], &rec)
		}
		if err == nil && rec.ID != id {
			err = errors.New("record does not match its file")
		}
		if err != nil {
			return nil, fmt.Errorf("tài khoản %s: %w", id, err)
		}
		records = append(records, rec)
	}
	return records, nil
}

func readProtected(data []byte, out any) error {
	plain, err := secure.Unprotect(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, out)
}

func writeProtected(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if data, err = secure.Protect(data); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(path, append([]byte(storeHeader), data...), 0o600)
}

// saveRecordLocked writes one account's record file.
func (s *store) saveRecordLocked(rec Record) error {
	path, err := s.recordPath(rec.ID)
	if err != nil {
		return err
	}
	return writeProtected(path, rec)
}

// saveIndexLocked writes the account order. Record files are written before
// the index that lists them and removed after it, so a crash in between
// leaves at most an unlisted file, never a listed account without one.
func (s *store) saveIndexLocked() error {
	if s.legacy {
		for _, rec := range s.records {
			if err := s.saveRecordLocked(rec); err != nil {
				return err
			}
		}
	}
	ids := make([]string, len(s.records))
	for i, rec := range s.records {
		ids[i] = rec.ID
	}
	if err := writeProtected(s.path, ids); err != nil {
		return err
	}
	s.legacy = false
	return nil
}

// list returns copies of all records in saved order.
func (s *store) list() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return append([]Record(nil), s.records...), nil
}

func (s *store) get(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return Record{}, err
	}
	for _, rec := range s.records {
		if rec.ID == id {
			return rec, nil
		}
	}
	return Record{}, errNotFound
}

func (s *store) add(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if err := s.saveRecordLocked(rec); err != nil {
		return err
	}
	s.records = append(s.records, rec)
	if err := s.saveIndexLocked(); err != nil {
		s.records = s.records[:len(s.records)-1]
		return err
	}
	return nil
}

// update applies change to the stored record and persists that record only.
func (s *store) update(id string, now time.Time, change func(*Record)) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return Record{}, err
	}
	for i := range s.records {
		if s.records[i].ID != id {
			continue
		}
		previous := s.records[i]
		change(&s.records[i])
		s.records[i].UpdatedAt = now.UTC().Format(time.RFC3339)
		err := s.saveRecordLocked(s.records[i])
		if err == nil && s.legacy {
			err = s.saveIndexLocked()
		}
		if err != nil {
			s.records[i] = previous
			return Record{}, err
		}
		return s.records[i], nil
	}
	return Record{}, errNotFound
}

func (s *store) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	for i := range s.records {
		if s.records[i].ID == id {
			previous := s.records
			s.records = append(append([]Record(nil), s.records[:i]...), s.records[i+1:]...)
			if err := s.saveIndexLocked(); err != nil {
				s.records = previous
				return err
			}
			if path, err := s.recordPath(id); err == nil {
				_ = os.Remove(path)
			}
			return nil
		}
	}
	return errNotFound
}
