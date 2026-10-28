package muse

import (
	"context"
	"strings"
	"testing"
	"time"
)

// submitLong queues one video of seconds through the app's own request.
func submitLong(t *testing.T, p *Provider, seconds int) MuseJob {
	t.Helper()
	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"x"}, Ratio: "9:16", Duration: seconds})
	if !res.Success {
		t.Fatalf("submit: %s", res.Error)
	}
	return res.Data[0]
}

func TestLongVideoMessage(t *testing.T) {
	msg := buildMessage(MuseJob{Kind: MuseJobVideo, Prompt: "a fox hunts in the snow", Ratio: "9:16", Duration: 30})
	for _, want := range []string{
		"Aspect ratio: 9:16 (portrait)\nDuration: 30 seconds\nLong video:",
		"Make it as 3 clips",
		"clip 1: 0-10s, clip 2: 10-20s, clip 3: 20-30s",
		"STATUS: DONE",
		"The prompt is the plan.",
		"opens as a film cut to a new camera angle",
		"\nPrompt:\na fox hunts in the snow",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	// Muse does not plan its own beats, clips never chain from the last frame
	// (the join looked shifted and stalled), and no genre's words (a soldier's
	// rank) slip into a guide every prompt gets.
	for _, unwanted := range []string{"beat", "extract the last frame", "Resume from the previous clip", "rank", "insignia", "name tape"} {
		if strings.Contains(msg, unwanted) {
			t.Errorf("message still has %q:\n%s", unwanted, msg)
		}
	}
	// One clip needs no guide; attached images replace the generated anchors.
	if short := buildMessage(MuseJob{Kind: MuseJobVideo, Prompt: "p", Duration: 10}); strings.Contains(short, "Long video") {
		t.Errorf("a 10-second video got the guide:\n%s", short)
	}
	if oneClip := strings.Join(shortVideoLines(MuseJob{Kind: MuseJobVideo}), "\n"); strings.Contains(oneClip, "rank") || strings.Contains(oneClip, "insignia") {
		t.Errorf("the one-clip guide names a genre's details:\n%s", oneClip)
	}
	withRefs := buildMessage(MuseJob{Kind: MuseJobVideo, Prompt: "p", Duration: 20, References: []string{"r"}})
	if !strings.Contains(withRefs, "The attached images are the anchors") || strings.Contains(withRefs, "Before clip 1, generate") {
		t.Errorf("a video with images got generated anchors:\n%s", withRefs)
	}
}

func TestParseLongStatus(t *testing.T) {
	cases := []struct {
		text          string
		state, detail string
		changed       string
	}{
		{"Video ready.\nSTATUS: DONE", statusDone, "", ""},
		{"**STATUS: INCOMPLETE** clips 5-6", statusIncomplete, "clips 5-6", ""},
		{"CHANGED: the storm is seen from the shore.\nSTATUS: DONE", statusDone, "", "the storm is seen from the shore"},
		{"STATUS: REFUSED - the video tool blocks the firing scenes.", statusRefused, "the video tool blocks the firing scenes", ""},
		{"STATUS: INCOMPLETE clips 3-4\n...\nSTATUS: DONE", statusDone, "", ""},
		{"Mình đang làm tiếp phần còn lại.", "", "", ""},
	}
	for _, c := range cases {
		got := parseLongStatus(c.text)
		if got.State != c.state || got.Detail != c.detail || got.Changed != c.changed {
			t.Errorf("parseLongStatus(%q) = %+v", c.text, got)
		}
	}
}

// Muse ran out of videos for one message: the app continues in the same chat,
// waits for the newer reply (the chat still shows the last turn's end at
// first) and keeps only the joined video.
func TestLongVideoContinuesInTheSameChat(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"completed"},
		quota: &MuseQuota{PercentUsed: 40},
		events: []AssistantEvent{
			{Text: "clip", Videos: []ResultMedia{{Path: "/out/c1.mp4", Mime: "video/mp4", ByteLen: 2}}},
			{Text: "Clips 1-4 are made.\nSTATUS: INCOMPLETE clips 5-6"},
		},
		files: map[string][]byte{"/out/c1.mp4": []byte("c1"), "/out/final.mp4": []byte("final")}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 60)
	reads := 0
	page.onSendTo = func() {
		// The reply arrives a few polls after the continue, not at once.
		p.sleep = func(ctx context.Context, _ time.Duration) error {
			if reads++; reads == 3 {
				page.mu.Lock()
				page.events = append(page.events,
					AssistantEvent{Text: "video", Videos: []ResultMedia{{Path: "/out/final.mp4", Mime: "video/mp4", ByteLen: 5}}},
					AssistantEvent{Text: "CHANGED: one shot is wider.\nSTATUS: DONE"})
				page.mu.Unlock()
			}
			return ctx.Err()
		}
	}

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || len(got.Files) != 1 || got.Continues != 1 || got.ChangeNote != "one shot is wider" {
		t.Fatalf("job = %+v", got)
	}
	if reads < 3 {
		t.Fatalf("the job ended on the old reply (reads = %d)", reads)
	}
	if len(page.sends) != 1 || len(page.sendTos) != 1 || !strings.HasPrefix(page.sendTos[0], "s1: Continue the same video project") {
		t.Fatalf("sends = %v, sendTos = %v", page.sends, page.sendTos)
	}
}

// Still incomplete after continueLimit: the job fails, never sent again in a
// new chat (that would pay for its clips twice).
func TestLongVideoStuckFails(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, quota: &MuseQuota{PercentUsed: 40},
		events: []AssistantEvent{{Text: "STATUS: INCOMPLETE clip 3"}}}
	page.onSendTo = func() {
		page.mu.Lock()
		page.events = append(page.events, AssistantEvent{Text: "Clip 3 still fails.\nSTATUS: INCOMPLETE clip 3"})
		page.mu.Unlock()
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 40)

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || got.SessionID != "s1" || got.Continues != continueLimit(got) || !strings.Contains(got.Error, "clip 3") {
		t.Fatalf("job = %+v", got)
	}
	if len(page.sends) != 1 || len(page.sendTos) != continueLimit(got) {
		t.Fatalf("sends = %d, sendTos = %d", len(page.sends), len(page.sendTos))
	}
}

// A continue nobody answers fails once its time is up; it is not re-sent.
func TestLongVideoUnansweredContinueFails(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, quota: &MuseQuota{PercentUsed: 40},
		events: []AssistantEvent{{Text: "STATUS: INCOMPLETE clips 5-6"}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 60)

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || !strings.Contains(got.Error, "không trả lời") {
		t.Fatalf("job = %+v", got)
	}
	if len(page.sends) != 1 || len(page.sendTos) != 1 || len(page.cancelled) != 0 {
		t.Fatalf("sends = %d, sendTos = %d, cancelled = %v", len(page.sends), len(page.sendTos), page.cancelled)
	}
}

// Muse refusing the prompt fails the job at once, with its reason.
func TestLongVideoRefused(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, quota: &MuseQuota{PercentUsed: 40},
		events: []AssistantEvent{{Text: "STATUS: REFUSED the video tool blocks every firing scene"}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 60)

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || !strings.Contains(got.Error, "blocks every firing scene") || len(page.sendTos) != 0 {
		t.Fatalf("job = %+v, sendTos = %v", got, page.sendTos)
	}
}

// An account out of usage between two clips keeps the chat and waits.
func TestLongVideoWaitsForItsAccount(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"},
		quota:  &MuseQuota{PercentUsed: 100, ResetsAt: "2099-01-01T00:00:00Z"},
		events: []AssistantEvent{{Text: "STATUS: INCOMPLETE clips 4-6"}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 60)

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobProcessing || got.SessionID != "s1" || got.NextCheckAt == "" || len(page.sendTos) != 0 {
		t.Fatalf("job = %+v, sendTos = %v", got, page.sendTos)
	}
}

// Muse ends its turn while still joining ("joining now, I will send the
// file"): no status line and no video is read again later, never a refusal.
func TestLongVideoStillJoiningIsReadAgain(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, quota: &MuseQuota{PercentUsed: 40},
		events: []AssistantEvent{{Text: "All 6 clips are made. Joining them now, I will send the file."}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 60)

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobProcessing || got.SessionID != "s1" || got.Rechecks != 1 || got.NextCheckAt == "" {
		t.Fatalf("job = %+v", got)
	}
}

// A script longer than the length: Muse makes nothing and names the length
// it needs; the job fails at once with it.
func TestLongVideoScriptTooShort(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, quota: &MuseQuota{PercentUsed: 40},
		events: []AssistantEvent{{Text: "The 35 lines need about 115 seconds.\nSTATUS: TOO SHORT 120 seconds"}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 60)

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || !strings.Contains(got.Error, "120 seconds") || len(page.sendTos) != 0 {
		t.Fatalf("job = %+v, sendTos = %v", got, page.sendTos)
	}
}

// A video of up to museVideosPerMessage clips needs no continue of its own;
// one past it (none is offered today) would need more messages in its chat.
func TestContinueLimitGrowsWithLength(t *testing.T) {
	for _, c := range []struct{ seconds, want int }{{20, spareContinues}, {60, spareContinues}, {70, 1 + spareContinues}} {
		if got := continueLimit(MuseJob{Kind: MuseJobVideo, Duration: c.seconds}); got != c.want {
			t.Errorf("continueLimit(%ds) = %d, want %d", c.seconds, got, c.want)
		}
	}
	if s := parseLongStatus("STATUS: TOO  SHORT 120 seconds"); s.State != statusTooShort || s.Detail != "120 seconds" {
		t.Errorf("TOO SHORT read as %+v", s)
	}
}
