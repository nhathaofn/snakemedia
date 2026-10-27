package flow

import (
	"maps"
	"slices"
	"sync"

	"SnakeMedia/internal/logger"
)

// Flow limits the data the app reads from Flow does not carry: GetModels lists
// models, qualities, reference counts, video ratios, durations and resolutions,
// but not these. Each value is what Flow's web client enforces, kept here as one
// named fallback; the first use in a run is logged so a change on Flow's side
// can be traced back to it. Read them only through the accessors below.
const (
	// fallbackMaxImagesPerRequest is the output count Flow's image editor offers.
	fallbackMaxImagesPerRequest = 4
	// fallbackMaxVideosPerRequest is the output count Flow's video editor offers.
	fallbackMaxVideosPerRequest = 4
	// fallbackMaxReferenceImageBytes is the largest reference image Flow's uploader takes.
	fallbackMaxReferenceImageBytes = 10 << 20
	// fallbackMaxSourceVideoBytes is the largest source video sent to Scotty.
	fallbackMaxSourceVideoBytes = 1 << 30
	// fallbackMinTrimSeconds is the shortest part of a source video Flow's trimmer keeps.
	fallbackMinTrimSeconds = 0.5
	// fallbackDefaultImageRatio is the ratio Flow's image editor starts with.
	fallbackDefaultImageRatio = "1:1"
	// fallbackDefaultBatchRatio is the ratio a new Image > All queue starts with.
	fallbackDefaultBatchRatio = "16:9"
)

var (
	// fallbackImageAspectRatios are the image ratios Flow's editor offers, in
	// display order (landscape to portrait); each has a wire code in FlowImageAspect.
	fallbackImageAspectRatios = []string{"16:9", "4:3", "1:1", "3:4", "9:16"}
	// fallbackReferenceMimeTypes are the reference image formats Flow's uploader takes.
	fallbackReferenceMimeTypes = []string{"image/png", "image/jpeg", "image/webp"}
	// fallbackSourceVideoTypes is Flow's upload whitelist for source videos, by
	// file extension, with the type sent when uploading.
	fallbackSourceVideoTypes = map[string]string{".mp4": "video/mp4", ".m4v": "video/m4v", ".webm": "video/webm", ".mov": "video/quicktime"}
)

var fallbacksUsed sync.Map

// fallback returns a built-in Flow limit, logging the first use of each per run.
func fallback[T any](name string, value T) T {
	if _, seen := fallbacksUsed.LoadOrStore(name, true); !seen {
		logger.Get().Info("flow limit not reported by Flow; using built-in value", "limit", name, "value", value)
	}
	return value
}

func maxImagesPerRequest() int {
	return fallback("maxImagesPerRequest", fallbackMaxImagesPerRequest)
}

func maxVideosPerRequest() int {
	return fallback("maxVideosPerRequest", fallbackMaxVideosPerRequest)
}

func maxReferenceImageBytes() int {
	return fallback("maxReferenceImageBytes", fallbackMaxReferenceImageBytes)
}

func maxSourceVideoBytes() int64 {
	return fallback[int64]("maxSourceVideoBytes", fallbackMaxSourceVideoBytes)
}

func minTrimSeconds() float64 {
	return fallback("minTrimSeconds", fallbackMinTrimSeconds)
}

// trimStepSeconds is one frame at the frame rate Flow assumes for uploads.
func trimStepSeconds() float64 {
	return 1.0 / uploadedVideoFPS
}

func imageAspectRatios() []string {
	return append([]string(nil), fallback("imageAspectRatios", fallbackImageAspectRatios)...)
}

func defaultImageRatio() string {
	return fallback("defaultImageRatio", fallbackDefaultImageRatio)
}

func defaultBatchRatio() string {
	return fallback("defaultBatchRatio", fallbackDefaultBatchRatio)
}

func referenceMimeTypes() []string {
	return append([]string(nil), fallback("referenceMimeTypes", fallbackReferenceMimeTypes)...)
}

// SourceVideoTypes is Flow's upload whitelist for source videos: extension to
// the content type sent when uploading.
func (s *Provider) SourceVideoTypes() map[string]string {
	return maps.Clone(fallback("sourceVideoTypes", fallbackSourceVideoTypes))
}

// SourceVideoExtensions lists the file extensions of SourceVideoTypes, sorted.
func (s *Provider) SourceVideoExtensions() []string {
	return slices.Sorted(maps.Keys(s.SourceVideoTypes()))
}
