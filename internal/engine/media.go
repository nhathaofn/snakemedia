package engine

import (
	"encoding/json"
	"mime"
	"path"
	"strings"
)

// MediaKind tells an image from a video.
type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaVideo MediaKind = "video"
)

// MediaRef is the value an image, video or media port carries: a file in the
// app's media store (served under /flow-media/), never the bytes themselves.
// Every provider hands its results on and takes its inputs in this form, so
// any image output can feed any image input.
type MediaRef struct {
	LocalURL string    `json:"localUrl"`
	Kind     MediaKind `json:"kind"`
	MimeType string    `json:"mimeType,omitempty"`
}

// videoTypes completes mime.TypeByExtension, whose table lacks video types on
// some systems. It names file formats, not what a platform accepts.
var videoTypes = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/x-m4v", ".mov": "video/quicktime", ".webm": "video/webm",
}

// RefFor describes a stored file from its name. kind is used when the name
// says nothing (no or unknown extension).
func RefFor(localURL string, kind MediaKind) MediaRef {
	ext := strings.ToLower(path.Ext(localURL))
	mimeType := videoTypes[ext]
	if mimeType == "" {
		mimeType = strings.TrimSpace(strings.Split(mime.TypeByExtension(ext), ";")[0])
	}
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		kind = MediaImage
	case strings.HasPrefix(mimeType, "video/"):
		kind = MediaVideo
	}
	return MediaRef{LocalURL: localURL, Kind: kind, MimeType: mimeType}
}

// Accepts reports whether a port of type t can take this file.
func (m MediaRef) Accepts(t PortType) bool {
	switch t {
	case PortMedia:
		return m.Kind == MediaImage || m.Kind == MediaVideo
	case PortImage:
		return m.Kind == MediaImage
	case PortVideo:
		return m.Kind == MediaVideo
	}
	return false
}

// Compatible reports whether an output of type from may feed an input of
// type to. A media input takes images and videos; a media output may hold
// either, so it only feeds media inputs.
func Compatible(from, to PortType) bool {
	if from == to {
		return true
	}
	return to == PortMedia && (from == PortImage || from == PortVideo)
}

// Texts is a text port that takes one value or many: a single prompt typed
// in a setting and a list handed on by another node read the same way.
type Texts []string

// UnmarshalJSON accepts a string or a list of strings.
func (t *Texts) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*t = Texts{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*t = many
	return nil
}

// NonEmpty drops blank entries and surrounding space.
func (t Texts) NonEmpty() []string {
	out := make([]string, 0, len(t))
	for _, s := range t {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
