//go:build !windows

// Package secure protects small local stores (accounts, imported cookies) at rest.
package secure

// Other platforms retain stores under owner-only directory/file permissions.
// Do not claim DPAPI encryption on platforms without the Windows credential API.
func Protect(data []byte) ([]byte, error)   { return data, nil }
func Unprotect(data []byte) ([]byte, error) { return data, nil }
