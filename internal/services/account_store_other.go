//go:build !windows

package services

// Other platforms retain the store under owner-only directory/file permissions.
// Do not claim DPAPI encryption on platforms without the Windows credential API.
func protectAccounts(data []byte) ([]byte, error)   { return data, nil }
func unprotectAccounts(data []byte) ([]byte, error) { return data, nil }
