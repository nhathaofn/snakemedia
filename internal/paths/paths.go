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
	museOutputDirName  = "muse-output"
	accountsDirName    = "accounts"
	updatesDirName     = "updates"
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
	// MuseOutputDir is the default folder finished Muse files are copied to.
	MuseOutputDir string
	// AccountsDir holds every provider's accounts, their Chrome profiles and
	// the proxy list.
	AccountsDir string
	// UpdatesDir holds a downloaded installer until it has run; it is under
	// the local (not roaming) cache folder where the OS has one.
	UpdatesDir string
}

// Resolve returns the standard per-user locations for appName, rooted at the
// OS user config directory (e.g. %AppData%\<appName> on Windows).
func Resolve(appName string) (Paths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("paths: resolve user config dir: %w", err)
	}
	p := ForRoot(filepath.Join(base, appName))
	if cache, err := os.UserCacheDir(); err == nil {
		p.UpdatesDir = filepath.Join(cache, appName, updatesDirName)
	}
	return p, nil
}

// ForRoot returns the layout for an explicit data directory (useful in tests).
func ForRoot(dataDir string) Paths {
	return Paths{
		DataDir:        dataDir,
		ConfigFile:     filepath.Join(dataDir, configFileName),
		LogDir:         filepath.Join(dataDir, logsDirName),
		MediaDir:       filepath.Join(dataDir, mediaDirName),
		ImageOutputDir: filepath.Join(dataDir, imageOutputDirName),
		MuseOutputDir:  filepath.Join(dataDir, museOutputDirName),
		AccountsDir:    filepath.Join(dataDir, accountsDirName),
		UpdatesDir:     filepath.Join(dataDir, updatesDirName),
	}
}
