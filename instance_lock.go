//go:build !dev

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"

	"SnakeMedia/internal/buildinfo"
)

// singleInstanceLock keeps one running app per user; a second launch focuses it.
func singleInstanceLock(info buildinfo.Info, app *App) *options.SingleInstanceLock {
	return &options.SingleInstanceLock{
		UniqueId:               info.SingleInstanceID(),
		OnSecondInstanceLaunch: app.onSecondInstanceLaunch,
	}
}
