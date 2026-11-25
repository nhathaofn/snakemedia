package media

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// BatchItem is what BatchFolder needs of one queued item of a batch form: the
// folder it saves into, its number there, when it was queued and whether it
// is still unfinished (held, queued or running).
type BatchItem struct {
	Dir       string
	Index     int
	CreatedAt string
	Open      bool
}

func normalizePathKey(dir string) string {
	clean := filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

// BatchFolder picks the folder under parent that prompts added on a batch
// form are saved into, and the number of the first of them. Prompts added
// while a batch into parent is still unfinished join it and continue its
// numbers, so a batch typed in several goes stays one folder numbered in
// prompt order; otherwise they open a new folder "<name>-<date-time>"
// numbered from 1. items are the queue's items of the same kind; the new
// folder is created with its first file.
func BatchFolder(parent, name string, now time.Time, items []BatchItem) (string, int) {
	parent = filepath.Clean(parent)
	parentKey := normalizePathKey(parent)
	open, openKey, openedAt := "", "", ""
	used := map[string]bool{}
	for _, it := range items {
		dirKey := normalizePathKey(it.Dir)
		used[dirKey] = true
		if it.Open && normalizePathKey(filepath.Dir(it.Dir)) == parentKey && it.CreatedAt >= openedAt {
			open, openKey, openedAt = filepath.Clean(it.Dir), dirKey, it.CreatedAt
		}
	}
	if open != "" {
		next := NextNumber(open)
		for _, it := range items {
			if normalizePathKey(it.Dir) == openKey && it.Index >= next {
				next = it.Index + 1
			}
		}
		return open, next
	}
	base := fmt.Sprintf("%s-%s", name, now.Format("20060102-150405"))
	dir := filepath.Join(parent, base)
	for i := 2; ; i++ {
		if _, err := os.Stat(dir); err != nil && !used[normalizePathKey(dir)] {
			return dir, 1
		}
		dir = filepath.Join(parent, fmt.Sprintf("%s-%d", base, i))
	}
}
