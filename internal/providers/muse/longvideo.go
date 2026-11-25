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

// longVideoLines provide only the technical multi-clip protocol for videos
// longer than one Muse clip (~10s): sequential clip spans, joining into one MP4,
// the per-message video call limit, and the STATUS line read by parseLongStatus.
func longVideoLines(job MuseJob) []string {
	n := clipCount(job.Duration)
	shape := ""
	if r, ok := ratioFor(job.Ratio, job.Kind); ok {
		shape = ", " + r.Value
	}
	return []string{
		fmt.Sprintf("Long video: generate %d sequential clips (%s) progressing through the prompt without repeating scenes, keep recurring characters and settings consistent across clean cuts (do not chain from the previous clip's last frame), and join them in order into one %d s MP4%s.", n, clipTable(job.Duration), job.Duration, shape),
		fmt.Sprintf("At most %d video generations are allowed per message; if clips remain at that limit, stop and I will ask you to continue in this chat.", museVideosPerMessage),
		"End your reply with CHANGED: <note> (only if a blocked shot had to be adjusted), then one line: STATUS: DONE (joined MP4 exported), STATUS: INCOMPLETE <missing clips>, or STATUS: REFUSED <reason>.",
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
const continueMessage = "Continue the same video in this chat: generate only the missing clips, join all clips in order into the complete MP4, and end with CHANGED if applicable and one STATUS line (STATUS: DONE or STATUS: INCOMPLETE <missing clips>)."

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
	statusRe  = regexp.MustCompile(`(?im)^[\s\p{P}*_]*STATUS[\s\p{P}*_]*:\s*(DONE|INCOMPLETE|REFUSED|TOO\s+SHORT)\b[\s:.\-–—*_]*(.*?)[\s\p{P}*_]*$`)
	changedRe = regexp.MustCompile(`(?im)^[\s\p{P}*_]*CHANGED[\s\p{P}*_]*:\s*(.+?)\s*$`)
)

// parseLongStatus reads the last status line (and CHANGED line) of a reply.
func parseLongStatus(text string) longStatus {
	var st longStatus
	if m := statusRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		last := m[len(m)-1]
		st.State, st.Detail = strings.Join(strings.Fields(strings.ToUpper(last[1])), " "), strings.TrimSpace(last[2])
	}
	if m := changedRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		changed := strings.TrimRight(strings.TrimSpace(m[len(m)-1][1]), ";.")
		if !isEmptyChangedValue(changed) {
			st.Changed = changed
		}
	}
	return st
}

func isEmptyChangedValue(s string) bool {
	clean := strings.TrimSpace(strings.TrimRight(s, ";."))
	return clean == "" ||
		strings.EqualFold(clean, "none") ||
		strings.EqualFold(clean, "n/a") ||
		clean == "-" ||
		strings.EqualFold(clean, "không") ||
		strings.EqualFold(clean, "không có")
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
