package muse

import (
	"fmt"
	"strings"
)

// Muse's chat agent reads the message and writes its own tool call to
// media.generate_image / media.generate_video. Keep the header free of
// directorial, pacing, audio, or negative creative rules, while supplying short
// session isolation and reference-grounding cues so attached subjects stay
// consistent and reference sheets are not rendered directly as frames.
var kindRequest = map[MuseJobKind]string{
	MuseJobImage: "Task: Create a new image.",
	MuseJobVideo: "Task: Create a new video.",
}

// requestRules isolates each generation chat from earlier conversations on the
// account without restricting how the prompt itself is directed.
const requestRules = "Standalone request: ignore earlier chats unless this message asks to reuse them."

// referenceLines provide concise identity grounding for plain attached images
// (no @tag/role metadata) while leaving scene, camera, and motion to the prompt.
func referenceLines(kind MuseJobKind, n int) []string {
	sheetGuard := ""
	motion := "composition and poses"
	if kind == MuseJobVideo {
		sheetGuard = " (do not show reference grids or plain backdrops as video frames)"
		motion = "camera and motion"
	}
	if n == 1 {
		return []string{fmt.Sprintf("Reference image 1: keep the exact look and design of the attached subject, while taking the scene, %s from the prompt%s.", motion, sheetGuard)}
	}
	return []string{fmt.Sprintf("Reference images (%s, in order): keep each attached subject's exact look and design as separate elements unless the prompt merges them, taking the scene, %s from the prompt%s.", imageNames(n), motion, sheetGuard)}
}

// imageNames lists n numbered images: "image 1 and image 2", or a range when many.
func imageNames(n int) string {
	if n <= 1 {
		return "image 1"
	}
	if n > 4 {
		return fmt.Sprintf("image 1 to image %d", n)
	}
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("image %d", i+1)
	}
	return strings.Join(names[:n-1], ", ") + " and " + names[n-1]
}

// buildMessage composes the chat message sent for a job: task line,
// standalone-request isolation, optional feature and reference grounding
// (storyboard frames, @tag/role or plain reference cues),
// requested aspect ratio and duration, the multi-clip protocol when Duration
// exceeds one clip, and finally the user's prompt unchanged.
func buildMessage(job MuseJob) string {
	lines := []string{kindRequest[job.Kind]}
	if job.Kind == MuseJobImage && job.BoardFrames > 0 {
		lines = append([]string{"Task: Create the storyboard images of a video."}, boardImageLines(job)...)
	}
	lines = append(lines, requestRules)
	first := 1
	if len(job.Frames) > 0 {
		lines = append(lines, boardVideoLines(job)...)
		first += len(job.Frames)
	}
	if n := len(job.References); n > 0 {
		switch {
		case len(job.RefInfo) == n:
			lines = append(lines, roleLines(job.Kind, job.RefInfo, first)...)
		case first > 1:
			lines = append(lines, roleLines(job.Kind, make([]MuseRefInfo, n), first)...)
		default:
			lines = append(lines, referenceLines(job.Kind, n)...)
		}
	}
	lines = append(lines, formatLines(job)...)
	if isLong(job) {
		lines = append(lines, longVideoLines(job)...)
	}
	return strings.Join(lines, "\n") + "\nprompt:\n" + job.Prompt
}
