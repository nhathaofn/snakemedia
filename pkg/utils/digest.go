package utils

import (
	"crypto/sha256"
	"io"
	"os"
)

// FileDigest returns a SHA-256 digest without holding the entire file in memory.
func FileDigest(path string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	in, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer in.Close()
	h := sha256.New()
	if _, err := io.Copy(h, in); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
