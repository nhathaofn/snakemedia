//go:build !windows

package chrome

import "os/exec"

func configureCmd(cmd *exec.Cmd) {}

// bindToApp is a no-op here; normal shutdown still closes app-owned browsers.
func bindToApp(cmd *exec.Cmd) error { return nil }
