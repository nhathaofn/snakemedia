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

// longVideoLines are the guide of a video made of several clips in one chat:
// the plan rule, how to split, keep and join the clips, the per-message limit
// and the status protocol parseLongStatus reads. Nothing in it names a genre,
// language or voice.
func longVideoLines(job MuseJob) []string {
	n := clipCount(job.Duration)
	shape := ""
	if r, ok := ratioFor(job.Ratio, job.Kind); ok {
		shape = ", " + r.Value
	}
	anchors := "Before clip 1, make an anchor image for each recurring person, object or place and reuse it in every clip it appears in."
	if len(job.References) > 0 || len(job.Frames) > 0 {
		anchors = "Use the attached images as anchors: storyboard frames for their assigned scenes, other images in their roles; make extra anchors only for recurring subjects they do not cover."
	}
	return []string{
		fmt.Sprintf("Long video: this is longer than one generated clip (about %d s). Make it as %d clips (%s) and join them into one MP4 of %d s%s.", museClipSeconds, n, clipTable(job.Duration), job.Duration, shape),
		planRule[MuseJobVideo],
		"1. Plan first: split the prompt over the clips without skipping, repeating or reordering anything, and put each clip's exact lines and speakers in that clip.",
		"2. Keep every recurring person, outfit, voice, place and style the same in all clips: write their descriptions once and reuse them word for word in each clip prompt. " + anchors,
		"3. From clip 2 on, each clip opens as a cut to a new camera angle, never from the previous clip's last frame; keep positions, props and light continuous.",
		"4. Join the clips in order as straight cuts at one resolution and frame rate, and check the final length.",
		fmt.Sprintf("5. One message allows at most %d successful video generations, retries included. If clips are still missing at that limit, keep them and stop; I will ask you to continue in this chat.", museVideosPerMessage),
		"6. Do not retry an unchanged refused clip. Use an allowed alternative that keeps the event and lines and say so in CHANGED, or stop with REFUSED.",
		fmt.Sprintf("If the lines cannot fit in %d seconds, make nothing and reply STATUS: TOO SHORT <seconds needed>.", job.Duration),
		"End your reply with CHANGED: <what you changed and why> (only if you changed something), then exactly one line: STATUS: DONE (the joined file is exported), STATUS: INCOMPLETE <clips missing>, STATUS: TOO SHORT <seconds needed> or STATUS: REFUSED <why>.",
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
// clips missing: same project, same plan, only the missing clips.
const continueMessage = "Continue the same video in this chat with the same plan, anchors and valid clips. Make only the missing or defective clips, then join and check the whole video. End with CHANGED if applicable and exactly one STATUS line as before; never DONE for a partial result."

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
