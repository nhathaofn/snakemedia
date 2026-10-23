// Command gofmtcheck exits non-zero when any Go file in the module is not gofmt-formatted.
// It is a cross-platform replacement for `test -z "$(gofmt -l .)"`.
//
// Usage: go run ./tools/gofmtcheck
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"frontend":     true,
	"build":        true,
}

func main() {
	var unformatted []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(src)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !bytes.Equal(src, formatted) {
			unformatted = append(unformatted, path)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gofmtcheck:", err)
		os.Exit(2)
	}
	if len(unformatted) > 0 {
		fmt.Fprintln(os.Stderr, "These files are not gofmt-formatted (run `gofmt -w .`):")
		for _, f := range unformatted {
			fmt.Fprintln(os.Stderr, "  "+f)
		}
		os.Exit(1)
	}
}
