//go:build !windows

package utils

import "os"

// PublishFile installs a staged file without replacing an existing target.
// The caller removes the staging path after use.
func PublishFile(src, dst string) error { return os.Link(src, dst) }
