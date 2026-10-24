//go:build !windows

package chrome

import (
	"errors"
	"syscall"
)

func isConnectionRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
