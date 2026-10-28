// Package buildinfo exposes application identity (name, version, build metadata).
//
// The single source of truth for name and version is wails.json, which is embedded
// by the main package and parsed here. Commit and build date can be injected at
// build time:
//
//	wails build -ldflags "-X SnakeMedia/internal/buildinfo.commit=<sha> -X SnakeMedia/internal/buildinfo.buildDate=<date>"
//
// When not injected, the commit falls back to the VCS revision recorded by the Go toolchain.
package buildinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
)

// Values overridable via -ldflags -X.
var (
	commit    = ""
	buildDate = ""
)

const unknown = "unknown"

// Info describes the running application build.
type Info struct {
	Name        string `json:"name"`
	ProductName string `json:"productName"`
	CompanyName string `json:"companyName"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	BuildDate   string `json:"buildDate"`
	// UpdateRepository is the GitHub "owner/name" whose releases the updater
	// reads (wails.json update.githubRepository); empty turns updates off.
	UpdateRepository string `json:"updateRepository"`
}

// repositoryPattern is a GitHub "owner/name".
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

// ErrInvalidManifest is returned when wails.json is missing required identity fields.
var ErrInvalidManifest = errors.New("buildinfo: invalid wails.json manifest")

type wailsManifest struct {
	Name string `json:"name"`
	Info struct {
		CompanyName    string `json:"companyName"`
		ProductName    string `json:"productName"`
		ProductVersion string `json:"productVersion"`
	} `json:"info"`
	Update struct {
		GitHubRepository string `json:"githubRepository"`
	} `json:"update"`
}

// Parse builds Info from the raw contents of wails.json.
func Parse(manifest []byte) (Info, error) {
	var m wailsManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return Info{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return Info{}, fmt.Errorf("%w: \"name\" is required", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Info.ProductVersion) == "" {
		return Info{}, fmt.Errorf("%w: \"info.productVersion\" is required", ErrInvalidManifest)
	}

	repo := strings.TrimSpace(m.Update.GitHubRepository)
	if repo != "" && !repositoryPattern.MatchString(repo) {
		return Info{}, fmt.Errorf("%w: \"update.githubRepository\" must be owner/name, got %q", ErrInvalidManifest, repo)
	}

	productName := m.Info.ProductName
	if productName == "" {
		productName = m.Name
	}

	return Info{
		Name:        m.Name,
		ProductName: productName,
		CompanyName: m.Info.CompanyName,
		Version:     m.Info.ProductVersion,
		Commit:      resolveCommit(),
		BuildDate:   valueOr(buildDate, unknown),

		UpdateRepository: repo,
	}, nil
}

// SingleInstanceID returns a stable identifier used to enforce one running instance.
func (i Info) SingleInstanceID() string {
	parts := []string{"app", i.Name}
	if i.CompanyName != "" {
		parts = []string{"app", i.CompanyName, i.Name}
	}
	return strings.ToLower(strings.Join(parts, "."))
}

func resolveCommit() string {
	if commit != "" {
		return commit
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return s.Value
			}
		}
	}
	return unknown
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
