// Package tools manages the external programs features run (FFmpeg and the
// like): where each one is found, and an on-demand download into the app's
// own tools folder when the machine has none. What exists, where it comes
// from and how it is checked is data in manifest.json, never code (ADR 0015).
package tools

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

//go:embed manifest.json
var manifestJSON []byte

// Manifest lists every tool the app knows.
type Manifest struct {
	Tools []Spec `json:"tools"`
}

// Spec is one tool: the programs it provides and, per platform, where the
// app may download it from.
type Spec struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Homepage string `json:"homepage"`
	// Programs are the executables' base names, without an extension.
	Programs []string `json:"programs"`
	// Sources are keyed by "<GOOS>/<GOARCH>"; a platform without one can only
	// use a copy found on the machine.
	Sources map[string]Source `json:"sources"`
}

// Source is where a tool's archive is downloaded from. URLs may name
// {version} (read from VersionURL) and SHA256URL may name {url}.
type Source struct {
	VersionURL string `json:"versionUrl,omitempty"`
	URL        string `json:"url"`
	// SHA256 pins the archive; otherwise SHA256URL is the publisher's checksum
	// file, whose first field is the archive's SHA-256.
	SHA256    string `json:"sha256,omitempty"`
	SHA256URL string `json:"sha256Url,omitempty"`
	// Archive is the format: "zip".
	Archive  string `json:"archive"`
	MaxBytes int64  `json:"maxBytes"`
}

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ParseManifest reads and checks a manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("tools: manifest: %w", err)
	}
	ids, programs := map[string]bool{}, map[string]bool{}
	for _, t := range m.Tools {
		if !idPattern.MatchString(t.ID) || ids[t.ID] || t.Name == "" || len(t.Programs) == 0 {
			return Manifest{}, fmt.Errorf("tools: manifest: tool %q needs a unique id, a name and programs", t.ID)
		}
		ids[t.ID] = true
		for _, p := range t.Programs {
			if !idPattern.MatchString(p) || programs[p] {
				return Manifest{}, fmt.Errorf("tools: manifest: program %q of %s is invalid or listed twice", p, t.ID)
			}
			programs[p] = true
		}
		for platform, s := range t.Sources {
			if err := s.check(); err != nil {
				return Manifest{}, fmt.Errorf("tools: manifest: %s %s: %w", t.ID, platform, err)
			}
		}
	}
	return m, nil
}

func (s Source) check() error {
	switch {
	case !strings.HasPrefix(s.URL, "https://"):
		return errors.New("url must be https")
	case s.VersionURL != "" && !strings.HasPrefix(s.VersionURL, "https://"):
		return errors.New("versionUrl must be https")
	case s.SHA256 == "" && !strings.HasPrefix(s.SHA256URL, "https://") && !strings.HasPrefix(s.SHA256URL, "{url}"):
		return errors.New("needs sha256 or an https sha256Url")
	case s.SHA256 != "" && !sha256Pattern.MatchString(s.SHA256):
		return errors.New("sha256 is not a lowercase SHA-256")
	case s.Archive != "zip":
		return fmt.Errorf("archive %q is not supported", s.Archive)
	case s.MaxBytes <= 0:
		return errors.New("maxBytes must be positive")
	}
	return nil
}

// source is this platform's download source, if the tool has one.
func (t Spec) source() (Source, bool) {
	s, ok := t.Sources[runtime.GOOS+"/"+runtime.GOARCH]
	return s, ok
}
