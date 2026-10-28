package muse

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"SnakeMedia/internal/media"
)

// MuseRatio is a frame shape a create screen may ask Muse for. Value is
// written into the message as is ("9:16", with its orientation in English);
// Label is the screens' word for it ("dọc").
type MuseRatio struct {
	Value string        `json:"value"`
	Label string        `json:"label"`
	Kinds []MuseJobKind `json:"kinds"`
}

// Muse's media tools take no ratio or duration parameter; a line in front of
// the prompt asks for them, and Muse follows it. Read live from muse.ai on
// 2026-10-27 (TestLiveFormatWording, one turn per case):
//   - images: 1:1, 3:4, 4:3 (and 2:3, 3:2, 4:5, 5:4) come out exact, 21:9 as
//     7:3; 9:16 and 16:9 come out near 9:14 and 14:9 (Muse's image model makes
//     nothing longer); with no line, 3:2. The screens offer Flow's image set;
//   - videos: 1:1, 9:16 and 16:9 exact (4:3, 3:4 too, 21:9 as 7:3); every
//     length from 2 to 10 seconds exact, 15 asked gives 10 (Muse's longest);
//     with no line, 16:9 and 10 seconds. The screens offer 9:16, 16:9, 1:1.
var museRatios = []MuseRatio{
	{Value: "16:9", Label: "ngang", Kinds: []MuseJobKind{MuseJobImage, MuseJobVideo}},
	{Value: "4:3", Label: "ngang", Kinds: []MuseJobKind{MuseJobImage}},
	{Value: "1:1", Label: "vuông", Kinds: []MuseJobKind{MuseJobImage, MuseJobVideo}},
	{Value: "3:4", Label: "dọc", Kinds: []MuseJobKind{MuseJobImage}},
	{Value: "9:16", Label: "dọc", Kinds: []MuseJobKind{MuseJobImage, MuseJobVideo}},
}

// museDurations are the video lengths (seconds) the screens offer: one clip
// (10 seconds, Muse's longest), or a long video Muse makes as several clips
// joined in one chat (longvideo.go), up to museVideosPerMessage clips, one
// message's worth. Read live on 2026-10-28: with the long video guide, 20,
// 30, 40 and 60 seconds came out exact, one cast and the prompt followed in
// order; without it Muse joined near copies of one clip.
var museDurations = []int{10, 20, 30, 40, 50, 60}

// Result checks: a file whose shape is off by more than ratioTolerance (or
// turned the other way), or whose length is off by more than
// durationTolerance seconds, gets a note. Muse's near-9:16 images (9:14) are
// within the tolerance on purpose: they are the closest Muse makes.
const (
	ratioTolerance    = 0.2
	durationTolerance = 1.0
)

// ratioFor returns the ratio value for kind, or false when it is not offered.
func ratioFor(value string, kind MuseJobKind) (MuseRatio, bool) {
	i := slices.IndexFunc(museRatios, func(r MuseRatio) bool { return r.Value == value })
	if i < 0 || !slices.Contains(museRatios[i].Kinds, kind) {
		return MuseRatio{}, false
	}
	return museRatios[i], true
}

// checkFormat refuses a ratio or duration the kind is not offered.
func checkFormat(kind MuseJobKind, ratio string, duration int) error {
	if ratio != "" {
		if _, ok := ratioFor(ratio, kind); !ok {
			return fmt.Errorf("Muse không có tỉ lệ %s cho %s.", ratio, kindName[kind])
		}
	}
	if duration != 0 && (kind != MuseJobVideo || !slices.Contains(museDurations, duration)) {
		return fmt.Errorf("Thời lượng %d giây không dùng được cho %s.", duration, kindName[kind])
	}
	return nil
}

// formatLines are what the message asks for besides the prompt:
// "Aspect ratio: 9:16 (portrait)" and "Duration: 8 seconds"; none when the
// job leaves both to the prompt.
func formatLines(job MuseJob) []string {
	var lines []string
	if r, ok := ratioFor(job.Ratio, job.Kind); ok {
		line := fmt.Sprintf("Aspect ratio: %s (%s)", r.Value, orientation(r.Value))
		if len(job.References) > 0 {
			// An attached image's own shape must not win over the ratio asked.
			line += fmt.Sprintf(". Frame the %s in this ratio, not in the shape of the attached images.", kindWord[job.Kind])
		}
		lines = append(lines, line)
	}
	if job.Kind == MuseJobVideo && job.Duration > 0 {
		lines = append(lines, fmt.Sprintf("Duration: %d seconds", job.Duration))
	}
	return lines
}

// orientation names a ratio's shape in the message ("portrait").
func orientation(value string) string {
	switch r, _ := parseRatio(value); {
	case r > 1:
		return "landscape"
	case r < 1:
		return "portrait"
	default:
		return "square"
	}
}

// parseRatio turns "9:16" into 9/16.
func parseRatio(value string) (float64, bool) {
	w, h, ok := strings.Cut(value, ":")
	if !ok {
		return 0, false
	}
	a, err1 := strconv.ParseFloat(w, 64)
	b, err2 := strconv.ParseFloat(h, 64)
	if err1 != nil || err2 != nil || a <= 0 || b <= 0 {
		return 0, false
	}
	return a / b, true
}

// formatNote compares one made file with what the job asked for; empty when
// it matches (or nothing was asked). seconds is 0 for an image.
func formatNote(job MuseJob, width, height int, seconds float64) string {
	var notes []string
	if want, ok := parseRatio(job.Ratio); ok && width > 0 && height > 0 {
		got := float64(width) / float64(height)
		turned := (want > 1.05 && got < 0.95) || (want < 0.95 && got > 1.05)
		if turned || math.Abs(got/want-1) > ratioTolerance {
			notes = append(notes, fmt.Sprintf("ra %d×%d thay vì %s", width, height, job.Ratio))
		}
	}
	if job.Kind == MuseJobVideo && job.Duration > 0 && seconds > 0 && math.Abs(seconds-float64(job.Duration)) > durationTolerance {
		notes = append(notes, fmt.Sprintf("dài %.0f giây thay vì %d", seconds, job.Duration))
	}
	if len(notes) == 0 {
		return ""
	}
	return "Muse " + strings.Join(notes, ", ") + "."
}

// measureNote measures the job's first downloaded file against what it asked
// for. A file that cannot be measured gets no note.
func (p *Provider) measureNote(ctx context.Context, job MuseJob, files []string) string {
	if (job.Ratio == "" && job.Duration == 0) || len(files) == 0 {
		return ""
	}
	path, err := p.media.Path(files[0])
	if err != nil {
		return ""
	}
	if job.Kind == MuseJobVideo {
		meta, err := media.ProbeVideo(ctx, path, p.cfg.Get().Flow)
		if err != nil {
			return ""
		}
		return formatNote(job, meta.Width, meta.Height, meta.DurationSeconds)
	}
	w, h, err := media.ImageSize(path)
	if err != nil {
		return ""
	}
	return formatNote(job, w, h, 0)
}
