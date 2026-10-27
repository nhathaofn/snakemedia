//go:build windows

// Package secure protects small local stores (accounts, imported cookies) at rest.
package secure

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Protect binds data to the current Windows user with DPAPI. No app key,
// password, cookie or token is embedded in the executable or configuration.
func Protect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty store")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}

// Unprotect reverses Protect for the same Windows user.
func Unprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty encrypted store")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOtherUser, err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
