package muse

import "fmt"

// The message is read by Muse's agent, not by the video model: the agent
// applies these lines itself and passes the prompt's scene content on to its
// media tool (seen in Muse's own account of its tool calls, TestLiveConsole,
// 2026-11-11). So the lines below only say what the agent must do; anything
// about the scene stays in the user's prompt. Shortening them from ~4,500 to
// ~700 characters changed neither the format, the references nor the lines of
// 18 test clips (docs/decisions/0016-muse-message-brief.md).

// requestRules open every new job's message: each request stands alone, even
// though Muse remembers earlier chats of the account.
const requestRules = "Use only this message and its attached images; ignore earlier chats and outputs unless this message asks to reuse them."

// planRule keeps the prompt authoritative for each kind. Speaker labels are
// named because Muse once read "X says:" aloud.
var planRule = map[MuseJobKind]string{
	MuseJobImage: "The prompt is the plan: keep its subjects, composition, style and any text it asks for exactly. Fill in only what it leaves open. Add no other subjects, captions or watermarks.",
	MuseJobVideo: "The prompt is the plan: keep its subjects, action order, camera, style and every quoted line word for word, in its language, by its speaker; speaker names are labels, not words to say. Fill in only what it leaves open. Add no other characters, events or speech, and no captions, subtitles, watermarks or music it does not ask for.",
}

// fitRule asks a one-clip video to refuse rather than cut lines to fit.
func fitRule(job MuseJob) string {
	length := "the clip"
	if job.Duration > 0 {
		length = fmt.Sprintf("%d seconds", job.Duration)
	}
	return "If the lines and actions cannot fit in " + length + ", make nothing and say how long is needed."
}

// referenceSheetRule keeps reference images out of a video's frames.
const referenceSheetRule = "Reference images are guidance, never frames: never show a reference sheet, its grid, multiple views or plain background, even at the start or end; frame 0 already shows the scene."

// videoReferenceLines add the sheet rule to a video with reference images
// (storyboard scene images have their own line).
func videoReferenceLines(job MuseJob) []string {
	if job.Kind != MuseJobVideo || len(job.References) == 0 {
		return nil
	}
	return []string{referenceSheetRule}
}

// shortVideoLines are the one-clip guide: the plan and the fit check.
func shortVideoLines(job MuseJob) []string {
	return []string{planRule[MuseJobVideo], fitRule(job)}
}
