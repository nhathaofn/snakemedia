//go:build !windows

package muse

import "os/exec"

func hideCmdWindow(cmd *exec.Cmd) {}
