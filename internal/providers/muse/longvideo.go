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

// speechWordsPerSecond is a brisk speaking pace, the one short-form dramas
// use, which tells Muse how many words one clip holds. At a slower pace Muse
// padded clips with pauses and found a one-minute script too long.
const speechWordsPerSecond = 3.5

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

// longVideoLines are the app's fixed guide for a long video, put in the
// message after the ratio and duration lines and before the user's prompt.
// Without it Muse makes every clip from the whole prompt and joins near
// copies (read live on 2026-10-28). The prompt is the plan: Muse lays its
// scenes, shots, timings and lines over the clips in order and directs only
// what the prompt leaves open, because users direct their own videos and
// Muse's own beats moved the weight away from the scenes that mattered.
// Clips join as cuts to a new angle, not from the previous clip's last frame,
// which made joins look shifted and stalled. The guide names no genre,
// language or voice. The account's saved rule already keeps each request on
// its own, so the guide does not repeat it. Its order is the one Muse asked
// for: product, the prompt as the plan, clip plan, continuity, anchors, cuts,
// sound, joining, priorities, blocked content, the per-message limit, the
// status line.
func longVideoLines(job MuseJob) []string {
	n := clipCount(job.Duration)
	shape := ""
	if r, ok := ratioFor(job.Ratio, job.Kind); ok {
		shape = fmt.Sprintf(", aspect ratio %s (%s)", r.Value, orientation(r.Value))
	}
	anchors := "Before clip 1, generate one image of every named character and every character seen in more than one clip, supporting roles included (a face left without an anchor changes between clips), and one of the main setting, from the continuity sheet, and attach the same images, in the same order, to every clip, saying what each is for (for example: image 1 gives the character's face and clothing only, image 2 gives the setting only, do not merge them)."
	if len(job.References) > 0 {
		anchors = "The attached images are the anchors: attach the same images, in the same order, to every clip, with the roles the Reference line gives them. Generate an extra anchor image for every named or recurring character, and the setting, that no attached image shows."
	}
	words := int(speechWordsPerSecond * museClipSeconds)
	return []string{
		fmt.Sprintf("Long video: the video must be %d seconds, longer than one generated clip (about %d seconds). Make it as %d clips of about %d seconds that follow the prompt in order, and join them into one file. Work in this order:",
			job.Duration, museClipSeconds, n, museClipSeconds),
		fmt.Sprintf("1. Final product: one MP4 file of exactly %d seconds%s, delivered as one video.", job.Duration, shape),
		fmt.Sprintf("2. The prompt is the plan. Make exactly what it describes, in its order, and nothing else. Where it divides the video into scenes or shots, keep their number, order and content; where it gives timings, camera directions, framing, actions or lines, use them as written. Spoken lines are said word for word by their own speaker. Never add, drop, merge, reorder, shorten or rewrite its scenes, shots, events or lines, and add no event, character or line it does not contain. Decide only what it leaves open (framing, camera and motion where it names none), in keeping with it. Before generating anything, check that its speech fits: at a brisk pace a %d-second clip holds about %d spoken words, with the action played while the lines are spoken. If its speech cannot be spoken within %d seconds, generate nothing and reply with STATUS: TOO SHORT <the seconds it needs>.",
			museClipSeconds, words, job.Duration),
		fmt.Sprintf("3. Clip plan, written before generating anything: lay the prompt's content, in its order, over the %d clips, using its own timings, or else %s. A scene or shot longer than a clip goes on in the next clip; short ones may share a clip; a clip boundary never skips, repeats or reorders anything. For each clip write: the part of the prompt it shows (copied, not rewritten), its shots and camera (the prompt's, or yours where it names none), the exact lines with their speakers, and the state of the action when it starts. A character, object or place is on screen only from the point the prompt brings it in and not after it leaves: nobody stands in the background of a shot before their entrance. No clip shows again what another clip shows; when the prompt has less content than the length, show its content across the clips in its order from different angles, inventing no new events. Pace is brisk unless the prompt asks otherwise: speech starts within the first second of a clip, lines follow each other without pauses, action happens while characters speak rather than before or after, and there are no slow establishing shots or lingering holds.",
			n, clipTable(job.Duration)),
		"4. Continuity sheet, written once and pasted word for word into every clip's prompt: each recurring character (name, role, age, gender, face, hair, build, clothing with colors and materials, anything worn that shows who they are matching what the prompt says about them, props, distinguishing marks, voice), each recurring object or product, the setting (place, time of day, weather, light, color palette) and the visual style (look, camera and lens, color grade), followed by the list of what must not change between clips. Take every detail the prompt gives (a name, a role, an age, a brand, a place) exactly as given. Keep the wording identical in every clip: rewording it is what makes characters drift.",
		"5. Anchors: " + anchors,
		"6. Cuts between clips: every clip from clip 2 on opens as a film cut to a new camera angle, a different shot size or side from the previous clip's last shot (the prompt's own shot when it names one). Never continue from, match or attach the previous clip's last frame, and do not resume from its snapshot: that makes the join look shifted and stalled. Across the cut keep what is shown continuous: the same characters in the same positions and clothes, the same props and light, the action at the point it had reached, with the continuity sheet and the anchors keeping the look.",
		"7. Sound follows the prompt first. Spoken words are in the prompt's language unless it names another, with numbers written as words:",
		"- Characters speaking on screen in the clips' own generated sound (when the prompt asks for it, or gives dialogue without a narrator): put the clip's lines in its prompt word for word, each with its speaker and the moment it is said, and ask for exactly those words, nothing added; keep that sound and add no narrator or text-to-speech voice. After each clip, check its speech against the plan (extract the audio and transcribe it if you can); remake a clip whose words are wrong when the per-message limit allows.",
		"- A narrator or voice-over: write the whole narration clip by clip, from the prompt's own words when it gives them, and voice it in one pass with one voice for the whole video (fitting the speaker the prompt describes, or the content when it describes none) at one speed; drop the clips' own generated speech and keep their other sound low under it. There is no lip sync with this voice: when a character speaks on screen, frame them wide, from the side or from behind.",
		"- No speech asked: keep each clip's natural sound and add no voice. Follow any music the prompt asks for.",
		fmt.Sprintf("8. Joining: bring every clip to the same resolution, frame rate and codec, join them in order as straight cuts, lay the voice track if there is one, and measure the result with ffprobe (%d seconds%s) before delivering it. Add no subtitles, captions, on-screen text, logos or channel names unless the prompt asks for them.", job.Duration, shape),
		"9. Priorities when they conflict: the prompt's content, order and exact lines > the same characters, voice, setting and style > the exact length > shots of your own. Do not stop to ask; decide from the prompt.",
		"10. Blocked content: when the video tool refuses a clip, do not report it missing and do not retry it unchanged. In the same message, make the closest version it accepts that still shows the same moment and keeps its lines word for word: soften step by step (show the impact, injury, force or restraint off screen, from a distance, through someone's reaction or by its result) until it passes, and say what you changed. INCOMPLETE is only for the per-message limit. If a clip still cannot be made at its mildest, reply STATUS: REFUSED <the clip and why>.",
		fmt.Sprintf("11. Limit: one message allows at most %d successful video generations, retries included. Make the clips in order; when the limit is reached before every clip is made, keep the clips made so far, do not join them, and stop: the next message continues from the first missing clip.", museVideosPerMessage),
		"End your reply with these lines, last:",
		"- CHANGED: <what you changed from the prompt and why>, only if you changed something under step 10;",
		"- then exactly one status line: STATUS: DONE, or STATUS: INCOMPLETE <the clips still missing, for example clips 7-12>, or STATUS: TOO SHORT <the seconds the prompt needs>, or STATUS: REFUSED <why>.",
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
const continueMessage = "Continue the same video project in this chat: keep the clip plan, continuity sheet, anchor images, voices and every clip already made, make only the clips still missing (a clip the video tool refused gets the closest version it accepts at once, never the same prompt again), then join and deliver the one complete video as the first message describes. End your reply with the CHANGED line (if any) and the STATUS line as before."

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

// shortVideoLines are the app's guide for a video of one clip (10 seconds or
// left to the prompt): the rules of the long video guide that still apply
// to one clip. The prompt is the plan, lines are said word for word, the
// pace is brisk, sound follows the prompt, details are taken exactly as
// given, and nothing is added. Like the long guide it names no genre,
// language or voice.
func shortVideoLines(job MuseJob) []string {
	words := int(speechWordsPerSecond * museClipSeconds)
	return []string{
		"Video rules: one clip, made from the prompt below.",
		"1. The prompt is the plan. Make exactly what it describes, in its order, and nothing else: its shots, timings, camera directions, framing, actions and lines as written. Never add, drop, merge, reorder, shorten or rewrite them, and add no event, character or line it does not contain. Decide only what it leaves open (framing, camera and motion where it names none), in keeping with it. A character, object or place is on screen only from the point the prompt brings it in and not after it leaves.",
		"2. Details exactly as given: names, roles and anything worn that shows them, ages, clothing, products, brands and places.",
		fmt.Sprintf("3. Pace is brisk unless the prompt asks otherwise: speech starts within the first second, lines follow each other without pauses (about %d words fit), action happens while characters speak, and there are no slow establishing shots or lingering holds.", words),
		"4. Sound follows the prompt. Spoken words are in the prompt's language unless it names another, said word for word by their own speaker, nothing added. Characters speaking on screen use the clip's own generated sound, with no narrator or text-to-speech voice, unless the prompt asks for a narrator or voice-over. If the prompt asks for no speech, keep the natural sound and add no voice. Follow any music it asks for.",
		"5. Add no subtitles, captions, on-screen text, logos or channel names unless the prompt asks for them.",
		"6. If the video tool refuses the prompt, make the closest version it accepts that still serves it (for example show the danger from a distance, through someone's reaction or by its aftermath), keep its lines unchanged, and say what you changed.",
	}
}
