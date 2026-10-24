//go:build windows

package chrome

import (
	"errors"
	"golang.org/x/sys/windows"
)

func isConnectionRefused(err error) bool { return errors.Is(err, windows.WSAECONNREFUSED) }
