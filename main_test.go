package main

import (
	"testing"

	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/config"
)

func TestEmbeddedManifestIsValid(t *testing.T) {
	info, err := buildinfo.Parse(manifest)
	if err != nil {
		t.Fatalf("wails.json is not a valid manifest: %v", err)
	}
	if info.SingleInstanceID() == "" {
		t.Error("SingleInstanceID must not be empty")
	}
}

func TestWindowBackgroundCoversAllThemes(t *testing.T) {
	for _, th := range config.AllThemes {
		if bg := windowBackground(th.Value); bg == nil || bg.A == 0 {
			t.Errorf("theme %q has no opaque window background", th.Value)
		}
	}
}
