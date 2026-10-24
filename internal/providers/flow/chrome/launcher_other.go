//go:build !windows

package chrome

import "os/exec"

func configureCmd(cmd *exec.Cmd) {}
