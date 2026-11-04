package muse

import "fmt"

// Keep the sheet rule separate from creative direction so it can be tuned
// without changing reference roles, locks or the user's prompt.
const referenceSheetRule = "Reference sheets are visual guidance only, never literal video frames.\n" +
	"Never display their grids, multiple views, neutral backgrounds or previews, including at the beginning and end.\n" +
	"Frame 0 must already show the requested scene.\n" +
	"If an opening image is required, create a clean single-scene frame from the references; never use the reference sheet itself."

// videoReferenceLines identify every affected attachment, including multiple
// images of the same asset. Frames are separately designated scene openings;
// first follows those frames and uses the same numbering as roleLines.
func videoReferenceLines(job MuseJob, first int) []string {
	if job.Kind != MuseJobVideo || len(job.References) == 0 {
		return nil
	}
	lines := []string{"Reference-sheet handling applies to these attached reference images:"}
	for k := range job.References {
		var info MuseRefInfo
		if len(job.RefInfo) == len(job.References) {
			info = job.RefInfo[k]
		}
		label := fmt.Sprintf("image %d", first+k)
		if info.Tag != "" {
			label += " = @" + info.Tag
		}
		role := roleNames[info.Role]
		if role == "" {
			role = "reference"
		}
		lines = append(lines, fmt.Sprintf("- %s (%s): extract only its assigned visual attributes; do not display the source sheet.", label, role))
	}
	lines = append(lines, referenceSheetRule)
	lines = append(lines, "An otherwise unassigned, clean single-scene image explicitly assigned by the prompt as an opening frame may initialize that scene. Separately identified storyboard opening frames initialize only their assigned scenes. A reference sheet, grid or multi-view image never qualifies as an opening frame.")
	return lines
}

// requestRules travel with each new job: account memory is an aid, not a
// prerequisite. Continuations use continueMessage in the existing chat.
const requestRules = "Request rules: treat this as an independent project. Use only this request and its attached files; reuse earlier work only when this request explicitly asks for it.\n" +
	"Intent: preserve the prompt's subjects, events, scene order, explicit timing, exact dialogue and language. Do not invent characters, events, claims or spoken lines. Complete unspecified composition, lighting and other visual details coherently without changing the meaning.\n" +
	"Authority: selected aspect ratio and duration are the output requirements. Reference roles and locks control only their assigned subjects and attributes; the prompt controls actions, dialogue and all remaining details. A reference is not a requirement to put its subject in every shot. Do not silently change a locked attribute.\n" +
	"Style: follow the requested medium, visual style and mood. Otherwise choose a coherent treatment suited to the content; do not force photorealism, animation, a genre or a promotional tone.\n" +
	"Text: add no unrequested captions, subtitles, watermarks or channel names. Preserve required product markings and explicitly requested text; do not treat them as decorative captions."

const pacingRule = "Pacing: follow the prompt's timing, pauses and mood. Otherwise choose a natural pace suited to the content. Do not force speech in the first second, rapid delivery, constant camera motion or slow motion. Allow time for actions, reactions and silence; do not pad with repeated events."

const speechFitRule = "Before generation, check whether the exact speech and required actions fit the duration, allowing for the requested language, delivery, pauses and reactions. Word counts are estimates, not a language-independent limit. Do not speed up unnaturally, omit lines or rewrite them to fit."

const audioRule = "Sound: follow the requested language, speakers, delivery, music and silence. Keep supplied dialogue word for word with its own speaker; do not translate or paraphrase it. Use on-screen generated speech for dialogue, and narration only when requested. If narration is requested without a script, write only what the request authorizes, with no new claims or events. Do not add speech to a request that supplies or asks for none. Keep voices consistent and background sound below speech; an explicit request for silence means no audio."
