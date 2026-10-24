//go:build !windows

package media

import "os/exec"

func hideProbeWindow(*exec.Cmd) {}
