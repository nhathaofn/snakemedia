//go:build windows

package updater

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

// createNoWindow gives the helper a console of its own that is never shown.
// PowerShell needs a console: started with DETACHED_PROCESS it exits at once,
// so the app closed and the update never ran. With its own process group the
// helper outlives the app.
const createNoWindow = 0x08000000

// Install starts a hidden helper that waits for this process to exit, runs
// installer silently into the running app's folder and starts the app again.
// The caller quits the app right after: the installer cannot replace an exe
// that is still running.
func (u *Updater) Install(installer string) error {
	if ok, reason := u.canInstall(); !ok {
		return fmt.Errorf("%s", reason)
	}
	exe, err := u.executable()
	if err != nil {
		return err
	}
	if _, err := os.Stat(installer); err != nil {
		return fmt.Errorf("Không thấy bộ cài đã tải: %w", err)
	}
	script := installScript(os.Getpid(), installer, filepath.Dir(exe), exe)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden", "-EncodedCommand", encodeCommand(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | syscall.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Không chạy được trình cài đặt: %w", err)
	}
	return cmd.Process.Release()
}

// installScript waits (up to two minutes) for the app to exit, installs over
// dir (NSIS /D= must come last and stay unquoted) and starts the app again,
// also when the install failed, so the user is never left without it.
func installScript(pid int, installer, dir, exe string) string {
	return strings.Join([]string{
		"$ErrorActionPreference = 'SilentlyContinue'",
		fmt.Sprintf("Wait-Process -Id %d -Timeout 120", pid),
		fmt.Sprintf("Start-Process -FilePath %s -ArgumentList %s -Wait", psQuote(installer), psQuote("/S /D="+dir)),
		fmt.Sprintf("Start-Process -FilePath %s", psQuote(exe)),
	}, "\n")
}

// psQuote makes s a PowerShell single-quoted literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// encodeCommand is PowerShell's -EncodedCommand form (UTF-16LE, base64),
// which needs no quoting on the command line.
func encodeCommand(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		buf[2*i], buf[2*i+1] = byte(u), byte(u>>8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
