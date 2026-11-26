//go:build windows

package utils

import (
	"os"

	"golang.org/x/sys/windows"
)

// PublishFile installs a staged file without replacing an existing target.
// The caller removes the staging path after use.
func PublishFile(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	if err := windows.MoveFile(from, to); err != nil {
		return &os.LinkError{Op: "move", Old: src, New: dst, Err: err}
	}
	return nil
}
