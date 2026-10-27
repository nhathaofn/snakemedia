package flow

import (
	"context"
	"slices"
	"testing"
)

func TestStudioConfigMatchesBackendLimits(t *testing.T) {
	p := New(nil, nil, nil)
	res := p.GetStudioConfig(context.Background())
	if !res.Success || res.Data.MaxImages != maxImagesPerRequest() || res.Data.MaxVideos != maxVideosPerRequest() ||
		res.Data.MaxReferenceBytes != maxReferenceImageBytes() || len(res.Data.ImageAspectRatios) != len(FlowImageAspect) {
		t.Fatalf("config out of step with validation: %+v", res)
	}
	for _, ratio := range append(res.Data.ImageAspectRatios, res.Data.DefaultImageRatio, res.Data.DefaultBatchRatio) {
		if _, ok := FlowImageAspect[ratio]; !ok {
			t.Errorf("offered ratio %q is not accepted by validation", ratio)
		}
	}
	if !slices.Equal(res.Data.VideoExtensions, p.SourceVideoExtensions()) {
		t.Errorf("video extensions %v differ from the upload whitelist %v", res.Data.VideoExtensions, p.SourceVideoExtensions())
	}
}

func TestFallbackLimitsAreCopies(t *testing.T) {
	ratios := imageAspectRatios()
	ratios[0] = "changed"
	types := (&Provider{}).SourceVideoTypes()
	types[".exe"] = "application/octet-stream"
	if imageAspectRatios()[0] == "changed" || (&Provider{}).SourceVideoTypes()[".exe"] != "" {
		t.Fatal("callers must not be able to change Flow's fallback limits")
	}
	if step := trimStepSeconds(); step <= 0 || step > minTrimSeconds() {
		t.Fatalf("trim step %v must be one frame, below the minimum %v", step, minTrimSeconds())
	}
}

func TestDisplayNameDropsDecoration(t *testing.T) {
	for raw, want := range map[string]string{"🍌 Nano Banana Pro": "Nano Banana Pro", "Veo 3.1 - Fast": "Veo 3.1 - Fast", "": ""} {
		if got := displayName(raw); got != want {
			t.Errorf("displayName(%q) = %q, want %q", raw, got, want)
		}
	}
}
