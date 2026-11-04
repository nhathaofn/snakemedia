package muse

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Muse's video tool makes one clip of about museClipSeconds per call, and
// Muse allows museVideosPerMessage successful video calls per message,
// retries included. Neither is exposed by muse.ai: both were read from Muse's
// own description of its tools on 2026-10-28 (TestLiveLongVideo, "ask").
const (
	museClipSeconds      = 10
	museVideosPerMessage = 6
)

// spareContinues are the "continue" messages a long video may send beyond the
// ones its clip count needs (museVideosPerMessage clips per message): they
// make up for clips Muse had to make again. More means it is stuck.
const spareContinues = 2

// continueLimit caps the continue messages a long video may send into its chat.
func continueLimit(job MuseJob) int {
	needed := (clipCount(job.Duration)+museVideosPerMessage-1)/museVideosPerMessage - 1
	return max(needed, 0) + spareContinues
}

// isLong reports whether a job is a video longer than one Muse clip, made as
// several clips joined in one chat.
func isLong(job MuseJob) bool {
	return job.Kind == MuseJobVideo && job.Duration > museClipSeconds
}

// clipCount is how many clips a long video is made of.
func clipCount(seconds int) int {
	return (seconds + museClipSeconds - 1) / museClipSeconds
}

// longVideoLines preserve the same-chat clip workflow and status protocol.
// Creative choices stay subordinate to the current request and reference roles.
func longVideoLines(job MuseJob) []string {
	n := clipCount(job.Duration)
	shape := ""
	if r, ok := ratioFor(job.Ratio, job.Kind); ok {
		shape = fmt.Sprintf(", aspect ratio %s (%s)", r.Value, orientation(r.Value))
	}
	anchors := "Before clip 1, generate anchors only for recurring visible characters, objects and settings not already covered by attached references. Match the requested medium and style. Reuse each anchor consistently, with its subject and role explicitly identified."
	if len(job.References) > 0 || len(job.Frames) > 0 {
		anchors = "The attached images are the anchors: use storyboard frames for their assigned scenes and other images only for their assigned reference roles. Generate extra anchors only for recurring visible subjects not covered by those images."
	}
	return []string{
		fmt.Sprintf("Long video: the video must be %d seconds, longer than one generated clip (about %d seconds). Make it as %d clips of about %d seconds that follow the prompt in order, and join them into one file. Work in this order:",
			job.Duration, museClipSeconds, n, museClipSeconds),
		fmt.Sprintf("1. Final product: one MP4 file of exactly %d seconds%s, delivered as one video. Selected output requirements override conflicting format values in the prompt; never omit required content to satisfy them.", job.Duration, shape),
		"2. The prompt is the plan. Preserve its scenes, shots, order, actions, explicit timing and exact lines with their speakers. Complete only unspecified visual details; do not add events or rewrite the story. " + speechFitRule + " If the required content cannot fit, generate nothing and reply STATUS: TOO SHORT <the estimated seconds needed and reason>.",
		fmt.Sprintf("3. Before generating, write a concise clip plan over %d clips, using the prompt's timings, or otherwise %s. For each clip identify the source scene, time range, shots, visible subjects, relevant references, exact dialogue and speakers, and starting and ending action state. A scene may span clips and short scenes may share a clip; boundaries must not skip, repeat or reorder content. Respect entrances and exits. Distribute brief content through its existing action and visual detail without inventing events. %s", n, clipTable(job.Duration), pacingRule),
		"4. Write one continuity sheet for recurring identities, outfits, products, voices, setting and style. Preserve supplied details and reference locks; fill only unspecified details. Paste the same relevant descriptions into each clip's prompt and record intentional scene changes separately. Continuity applies where subjects recur, not as a requirement to show every subject in every shot.",
		"5. Anchors: " + anchors + " Keep stable names and image ordering when reused. State which subjects are visible in each clip and which images supply attributes only. Never combine different identities or apply one subject's lock to another.",
		"6. Cuts between clips: every clip from clip 2 on opens as a film cut to a new camera angle, using the prompt's own shot when specified. Never attach or resume from the previous clip's last frame. Preserve screen direction, positions, props, light and the reached action state across cuts. Storyboard opening frames belong to their assigned scenes, not to the end of the previous clip. If an explicit uninterrupted shot cannot be delivered by this cut-based workflow, report the limitation rather than claiming an unbroken take.",
		"7. " + audioRule,
		"- For on-screen dialogue, place the exact lines, speakers and timing in each clip prompt. Check generated speech against the script when transcription or listening tools are available. Do not claim word-perfect speech without checking it.",
		"- For requested narration, create a consistent voice track for the whole video when supported. Keep requested character dialogue; lower competing sound under speech. Do not replace dialogue with narration or change framing to conceal an unrequested voice replacement.",
		fmt.Sprintf("8. Joining and validation: normalize resolution, frame rate and codec, join clips in order as straight cuts, and mix requested audio. Measure the final file with available media tools: target %d seconds%s. Check scene coverage, continuity, speech, unwanted text and the exported file. Correct defects within the remaining generation budget without regenerating valid clips. Clearly report checks you could not perform; never invent tool use or measurements.", job.Duration, shape),
		"9. Conflicts: use the request's attribute-specific authority rules. Preserve required content and exact lines; do not silently relax reference locks, timing or output requirements. Report an unresolved production limitation and do not claim completion. Make reasonable choices only for unspecified creative details.",
		"10. Blocked content: do not retry an unchanged rejected clip. When a permitted alternative can preserve the intended event and exact lines, use it and disclose the change in CHANGED. Do not change the story or required dialogue to obtain an output. If no such version is possible, reply STATUS: REFUSED <the clip and reason>. INCOMPLETE is reserved for clips left because of the per-message generation limit.",
		fmt.Sprintf("11. Limit: one message allows at most %d successful video generations, retries included. Make clips in order, retain valid clips and account for remakes. When the limit is reached with clips missing, keep completed work, do not join a partial result, and stop for continuation in this chat.", museVideosPerMessage),
		"End your reply with these lines, last:",
		"- CHANGED: <what changed from the prompt and why>, only if an allowed adaptation was made; report verification limitations separately before these final lines;",
		"- then exactly one status line: STATUS: DONE only when the complete joined file is exported and available; STATUS: INCOMPLETE <the clips still missing> only for the per-message limit; STATUS: TOO SHORT <the seconds the prompt needs>; or STATUS: REFUSED <why>. Never mark a plan, partial render or missing file DONE. For other unresolved production failures, explain the failure without inventing a status or claiming DONE.",
	}
}

// clipTable words the time span of every clip: "clip 1: 0-10s, clip 2: 10-20s".
func clipTable(seconds int) string {
	n := clipCount(seconds)
	spans := make([]string, n)
	for k := range spans {
		end := min((k+1)*museClipSeconds, seconds)
		spans[k] = fmt.Sprintf("clip %d: %d-%ds", k+1, k*museClipSeconds, end)
	}
	return strings.Join(spans, ", ")
}

// continueMessage is sent into a long video's chat when Muse stopped with
// clips missing. It says the request continues this project, which the
// account's saved rule asks for before Muse reuses anything it made.
const continueMessage = "Continue the same video project in this chat under the first request's rules: retain its clip plan, continuity sheet, reference roles, locks, anchors, voices and valid clips. Make only missing clips or confirmed defective clips within the remaining per-message limit; do not restart, repeat completed scenes or change the script. Join and export the complete video only after all required clips exist, then validate the final file. Disclose allowed adaptations and checks you could not perform. End with CHANGED if applicable and exactly one STATUS line using the first request's meanings; never report DONE for a partial result."

// longStatus is how Muse said a long video ended, read from the last lines
// of its reply.
type longStatus struct {
	// State is statusDone, statusIncomplete, statusTooShort, statusRefused, or empty when the
	// reply has no status line.
	State string
	// Detail is the rest of the status line: the clips missing or the reason.
	Detail string
	// Changed is the CHANGED line: what Muse changed from the prompt.
	Changed string
}

const (
	statusDone       = "DONE"
	statusIncomplete = "INCOMPLETE"
	statusRefused    = "REFUSED"
	statusTooShort   = "TOO SHORT"
)

var (
	statusRe  = regexp.MustCompile(`(?im)^\W*STATUS\W*:\s*(DONE|INCOMPLETE|REFUSED|TOO\s+SHORT)\b[\s:.\-–—*_]*(.*?)\W*$`)
	changedRe = regexp.MustCompile(`(?im)^\W*CHANGED\W*:\s*(.+?)\s*$`)
)

// parseLongStatus reads the last status line (and CHANGED line) of a reply.
func parseLongStatus(text string) longStatus {
	var st longStatus
	if m := statusRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		last := m[len(m)-1]
		st.State, st.Detail = strings.Join(strings.Fields(strings.ToUpper(last[1])), " "), strings.TrimSpace(last[2])
	}
	if m := changedRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		st.Changed = strings.TrimRight(strings.TrimSpace(m[len(m)-1][1]), ";.")
	}
	return st
}

// repliesAfter is the whole text of the chat's answers after the first seen
// ones, the answer to the latest message (replyText shortens and would cut the
// status line off).
func repliesAfter(events []AssistantEvent, seen int) string {
	if seen > len(events) {
		seen = len(events)
	}
	var texts []string
	for _, e := range events[seen:] {
		if t := strings.TrimSpace(e.Text); t != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, "\n")
}

// continueNeeded tells execute to send continueMessage into the chat; seen is
// how many chat events were read.
type continueNeeded struct{ seen int }

func (e *continueNeeded) Error() string {
	return "Muse chưa làm đủ các clip, cần nhắc làm tiếp"
}

// longIncompleteError is a long video Muse did not finish after continueLimit
// continue messages (or one left unanswered). It fails at once: the clips
// already made stay in the chat, and a new chat would pay for them again.
type longIncompleteError struct {
	continues int
	detail    string
}

func (e *longIncompleteError) Error() string {
	msg := fmt.Sprintf("Muse chưa làm xong video dài sau %d lần nhắc làm tiếp", e.continues)
	if e.detail != "" {
		msg += " (" + e.detail + ")"
	}
	return msg + ". Các clip đã làm vẫn nằm trong chat trên Muse; chạy lại sẽ làm từ đầu và tốn thêm lượt."
}

// waitAccountError holds a long video whose account ran out of usage between
// two clips: it keeps its chat and goes on there once the rest ends, since
// another account cannot read the chat and would pay for the clips again.
type waitAccountError struct{ until time.Time }

func (e *waitAccountError) Error() string {
	return "Tài khoản hết lượt Muse giữa chừng video dài; chờ tới " + e.until.Local().Format("15:04 02/01") + " để làm tiếp trong cùng chat."
}

// tooShortError is a script Muse checked before making anything and found
// longer than the asked length: no clip was made, and a longer length is
// needed, so it fails at once with the length Muse named.
type tooShortError struct{ need string }

func (e *tooShortError) Error() string {
	msg := "Kịch bản dài hơn thời lượng đã chọn nên Muse chưa tạo video nào"
	if e.need != "" {
		msg += " (cần khoảng " + e.need + ")"
	}
	return msg + ". Chọn thời lượng dài hơn hoặc rút gọn kịch bản rồi chạy lại."
}

// shortVideoLines add only the workflow needed for one generated clip.
func shortVideoLines(_ MuseJob) []string {
	return []string{
		"Video rules: one clip, made from the prompt below.",
		"1. The prompt is the plan. Preserve its shots, order, explicit timing, camera directions, actions and exact dialogue. Complete only unspecified visual details. Respect entrances and exits and apply reference roles only to the relevant subjects.",
		"2. " + speechFitRule + " If it cannot fit, do not generate a shortened or incomplete substitute; explain the duration needed.",
		"3. " + pacingRule,
		"4. " + audioRule,
		"5. Check the exported clip against the request: content, references, requested style, ratio, duration, speech and unwanted text. State which checks could not be performed; do not claim measurements or exact speech without verification.",
		"6. If the tool refuses, do not repeat the same rejected request. Use a permitted alternative only when it preserves the intended event and required lines, and disclose the change. Otherwise explain the refusal instead of silently replacing the content.",
	}
}
