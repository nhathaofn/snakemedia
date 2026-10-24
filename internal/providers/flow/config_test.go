package flow

import (
	"context"
	"testing"
)

func TestStudioConfigMatchesBackendLimits(t *testing.T) {
	res := New(nil, nil, nil).GetStudioConfig(context.Background())
	if !res.Success || res.Data.MaxImages != MaxImagesPerRequest || res.Data.MaxVideos != MaxVideosPerRequest ||
		res.Data.MaxReferenceBytes != MaxReferenceImageBytes || len(res.Data.ImageAspectRatios) != len(FlowImageAspect) {
		t.Fatalf("config out of step with validation: %+v", res)
	}
	for _, ratio := range res.Data.ImageAspectRatios {
		if _, ok := FlowImageAspect[ratio]; !ok {
			t.Errorf("offered ratio %q is not accepted by validation", ratio)
		}
	}
}

func TestDisplayNameDropsDecoration(t *testing.T) {
	for raw, want := range map[string]string{"🍌 Nano Banana Pro": "Nano Banana Pro", "Veo 3.1 - Fast": "Veo 3.1 - Fast", "": ""} {
		if got := displayName(raw); got != want {
			t.Errorf("displayName(%q) = %q, want %q", raw, got, want)
		}
	}
}
