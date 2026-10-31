package config

import (
	"fmt"
	"path/filepath"
)

// ToolsConfig is how the user points the app at external programs
// (internal/tools). The tools themselves, their sources and checksums are
// in the tools manifest, never here.
type ToolsConfig struct {
	// Paths maps a program name ("ffmpeg") to the executable the user chose;
	// a program not listed is looked up on the machine, then in the app's
	// tools folder.
	Paths map[string]string `json:"paths"`
}

func DefaultToolsConfig() ToolsConfig {
	return ToolsConfig{Paths: map[string]string{}}
}

func (c ToolsConfig) Validate() error {
	for program, path := range c.Paths {
		if program == "" || (path != "" && !filepath.IsAbs(path)) {
			return fmt.Errorf("tools: path of %q must be absolute", program)
		}
	}
	return nil
}
