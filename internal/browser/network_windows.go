//go:build windows

package browser

import (
	"errors"
	"golang.org/x/sys/windows"
)

func isConnectionRefused(err error) bool { return errors.Is(err, windows.WSAECONNREFUSED) }
