//go:build dev

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"

	"SnakeMedia/internal/buildinfo"
)

// singleInstanceLock is off under `wails dev`: every rebuild starts the new
// binary before the old one is gone, and a lock made the new build hand off to
// the stale window and exit, so code changes never showed up.
func singleInstanceLock(buildinfo.Info, *App) *options.SingleInstanceLock {
	return nil
}
