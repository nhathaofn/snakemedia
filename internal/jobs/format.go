package jobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// FormatVersion is the on-disk format of journals and of the job records
// providers save on their own (Flow's per-job files). Raise it when a saved
// field changes meaning or type, and read the older format on load. A file
// without a version predates versioning and is read as format 1.
// Version 2 records partial Muse output saves by source URL so older builds
// cannot mistake a compact saved-file list for a fully saved batch.
// Version 3 extends MuseAsset with Project, Type, and structured Items (angles/variants).
// Version 4 keeps IDs of retired Graph jobs that may have been billed.
const FormatVersion = 4

// ErrNewerFormat marks data saved by a newer build of the app (after the user
// went back to an older one). It is kept as it is and never overwritten: the
// newer build may hold jobs Flow or Muse already took.
var ErrNewerFormat = errors.New("dữ liệu được lưu bởi bản app mới hơn")

// formatHead is the version field every versioned file carries.
type formatHead struct {
	FormatVersion int `json:"formatVersion"`
}

// CheckFormat reports ErrNewerFormat for a JSON object saved in a format this
// build does not know. Anything else (an array, no version) is fine.
func CheckFormat(data []byte) error {
	var head formatHead
	if !isObject(data) || json.Unmarshal(data, &head) != nil {
		return nil
	}
	if head.FormatVersion > FormatVersion {
		return fmt.Errorf("%w (định dạng %d, bản này đọc tới %d); hãy cập nhật app", ErrNewerFormat, head.FormatVersion, FormatVersion)
	}
	return nil
}

// Stamp adds or replaces the current FormatVersion on a JSON object, removing any
// pre-existing formatVersion key so decoders never read an older trailing value.
func Stamp(data []byte) []byte {
	if !isObject(data) {
		return data
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Contains(trimmed, []byte(`"formatVersion"`)) {
		var obj map[string]json.RawMessage
		if json.Unmarshal(trimmed, &obj) == nil {
			delete(obj, "formatVersion")
			if stripped, err := json.Marshal(obj); err == nil {
				trimmed = stripped
			}
		}
	}
	rest := trimmed[1:]
	out := fmt.Appendf(nil, `{"formatVersion":%d`, FormatVersion)
	if len(bytes.TrimSpace(rest)) > 1 { // more than the closing brace
		out = append(out, ',')
	}
	return append(out, rest...)
}

func isObject(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) >= 2 && data[0] == '{'
}
