//go:build windows

package updater

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestInstallScriptQuotesPathsAndPutsTheFolderLast(t *testing.T) {
	script := installScript(42, `C:\Users\O'Neil\AppData\Demo-setup.exe`, `C:\Users\O'Neil\Programs\Demo`, `C:\Users\O'Neil\Programs\Demo\Demo.exe`)
	for _, want := range []string{
		"Wait-Process -Id 42",
		`-FilePath 'C:\Users\O''Neil\AppData\Demo-setup.exe'`,
		`-ArgumentList '/S /D=C:\Users\O''Neil\Programs\Demo' -Wait`,
		`Start-Process -FilePath 'C:\Users\O''Neil\Programs\Demo\Demo.exe'`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
}

func TestEncodeCommandIsUTF16Base64(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(encodeCommand("Đ ok"))
	if err != nil || len(raw)%2 != 0 {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	if got := string(utf16.Decode(units)); got != "Đ ok" {
		t.Fatalf("decoded %q", got)
	}
}
