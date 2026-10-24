//go:build windows

package media

import (
	"os/exec"
	"syscall"
)

func hideProbeWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
