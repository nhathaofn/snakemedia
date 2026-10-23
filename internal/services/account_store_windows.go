//go:build windows

package services

import (
	"errors"
	"golang.org/x/sys/windows"
	"unsafe"
)

// DPAPI binds the encrypted account store to the current Windows user. No app
// key, password, cookie or token is embedded in the executable or configuration.
func protectAccounts(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty account store")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}

func unprotectAccounts(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty encrypted account store")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
