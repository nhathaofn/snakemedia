//go:build windows

package chrome

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func findWindowsChrome() string {
	// 1. Check Registry HKLM
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		val, _, err := k.GetStringValue("")
		if err == nil && val != "" {
			if _, statErr := os.Stat(val); statErr == nil {
				return val
			}
		}
	}

	// 2. Check Registry HKCU
	k, err = registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		val, _, err := k.GetStringValue("")
		if err == nil && val != "" {
			if _, statErr := os.Stat(val); statErr == nil {
				return val
			}
		}
	}

	// 3. Check standard directory locations
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("LocalAppData"), "Google", "Chrome", "Application", "chrome.exe"),
	}
	for _, c := range candidates {
		if c != "" {
			if _, statErr := os.Stat(c); statErr == nil {
				return c
			}
		}
	}

	return ""
}
