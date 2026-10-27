//go:build windows

package main

import "golang.org/x/sys/windows"

// showFatal shows a blocking error box; it works before the Wails runtime
// (and its dialogs) is up.
func showFatal(title, message string) {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	m, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}
