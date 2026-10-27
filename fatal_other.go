//go:build !windows

package main

import (
	"fmt"
	"os"
)

// showFatal writes the error to stderr; only the Windows build shows a box.
func showFatal(title, message string) {
	_, _ = fmt.Fprintf(os.Stderr, "%s\n%s\n", title, message)
}
