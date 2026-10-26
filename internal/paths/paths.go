// Package paths resolves the per-user directories the application writes to.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	configFileName     = "config.json"
	logsDirName        = "logs"
	mediaDirName       = "media"
	imageOutputDirName = "generated-images"
)

// Paths groups the resolved on-disk locations for one application.
type Paths struct {
	DataDir    string
	ConfigFile string
	LogDir     string
	// MediaDir holds downloaded and imported media served at /flow-media/.
	MediaDir string
	// ImageOutputDir is the default folder Image > All saves batches into.
	ImageOutputDir string
}

// Resolve returns the standard per-user locations for appName, rooted at the
// OS user config directory (e.g. %AppData%\<appName> on Windows).
func Resolve(appName string) (Paths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("paths: resolve user config dir: %w", err)
	}
	return ForRoot(filepath.Join(base, appName)), nil
}

// ForRoot returns the layout for an explicit data directory (useful in tests).
func ForRoot(dataDir string) Paths {
	return Paths{
		DataDir:        dataDir,
		ConfigFile:     filepath.Join(dataDir, configFileName),
		LogDir:         filepath.Join(dataDir, logsDirName),
		MediaDir:       filepath.Join(dataDir, mediaDirName),
		ImageOutputDir: filepath.Join(dataDir, imageOutputDirName),
	}
}
