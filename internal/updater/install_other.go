//go:build !windows

package updater

import "errors"

// Install is Windows only: other platforms update by hand.
func (u *Updater) Install(string) error {
	return errors.New("Tự cập nhật chỉ hỗ trợ Windows.")
}
