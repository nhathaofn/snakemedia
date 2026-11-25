package muse

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMessageAsksForRatioAndDuration(t *testing.T) {
	cases := []struct {
		job  MuseJob
		want string
	}{
		{MuseJob{Kind: MuseJobImage, Prompt: "p"}, "Task: Create a new image.\n" + requestRules + "\nprompt:\np"},
		{MuseJob{Kind: MuseJobImage, Prompt: "p", Ratio: "3:4"},
			"Task: Create a new image.\n" + requestRules + "\nAspect ratio: 3:4 (portrait)\nprompt:\np"},
		{MuseJob{Kind: MuseJobVideo, Prompt: "p", Ratio: "16:9", Duration: 10},
			"Task: Create a new video.\n" + requestRules + "\nAspect ratio: 16:9 (landscape)\nDuration: 10 seconds\nprompt:\np"},
		{MuseJob{Kind: MuseJobVideo, Prompt: "p", Duration: 10}, "Task: Create a new video.\n" + requestRules + "\nDuration: 10 seconds\nprompt:\np"},
		// An image never asks for a length, nor for a ratio it is not offered.
		{MuseJob{Kind: MuseJobImage, Prompt: "p", Duration: 5}, "Task: Create a new image.\n" + requestRules + "\nprompt:\np"},
		// Plain attached images carry a concise grounding cue plus the ratio framing note.
		{MuseJob{Kind: MuseJobVideo, Prompt: "p", References: []string{"r"}, Ratio: "1:1", Duration: 10},
			"Task: Create a new video.\n" +
				requestRules + "\n" +
				"Reference image 1: keep the exact look and design of the attached subject, while taking the scene, camera and motion from the prompt (do not show reference grids or plain backdrops as video frames).\n" +
				"Aspect ratio: 1:1 (square). Frame the video in this ratio, not in the shape of the attached images.\n" +
				"Duration: 10 seconds\nprompt:\np"},
	}
	for _, c := range cases {
		if got := buildMessage(c.job); got != c.want {
			t.Errorf("message %q, want %q", got, c.want)
		}
	}
}

func TestPlainReferencesGroundEachImageConcisely(t *testing.T) {
	for n, names := range map[int]string{2: "image 1 and image 2", 3: "image 1, image 2 and image 3", 7: "image 1 to image 7"} {
		refs := make([]string, n)
		got := buildMessage(MuseJob{Kind: MuseJobImage, Prompt: "p", References: refs})
		want := "Task: Create a new image.\n" +
			requestRules + "\n" +
			"Reference images (" + names + ", in order): keep each attached subject's exact look and design as separate elements unless the prompt merges them, taking the scene, composition and poses from the prompt.\n" +
			"prompt:\np"
		if got != want {
			t.Errorf("%d plain references mismatch:\ngot:\n%s\nwant:\n%s", n, got, want)
		}
	}
}

// Attached files are named image_N and normalized to JPEG when Muse accepts .jpg.
func TestAttachmentsAreNamedAsNumbered(t *testing.T) {
	p := testProvider(t, &fakePage{})
	a, _ := p.media.Put("muse-ref-a", "image/png", ".png", testPNGBytes(t, 16, 16))
	b, _ := p.media.Put("muse-ref-b", "image/png", ".png", testPNGBytes(t, 16, 16))
	pngCaps := &MuseCapabilities{MaxFiles: 10, MaxFileBytes: 1 << 20, ImageTypes: []MuseFileType{{Mime: "image/png", Extensions: []string{".png"}}}}
	atts, err := p.attach(MuseJob{References: []string{a, b}}, pngCaps)
	if err != nil || len(atts) != 2 || atts[0].Filename != "image_1.png" || atts[1].Filename != "image_2.png" {
		t.Fatalf("png attachments %+v, err %v", atts, err)
	}
	jpgCaps := testCaps()
	jpgAtts, err := p.attach(MuseJob{References: []string{a, b}}, jpgCaps)
	if err != nil || len(jpgAtts) != 2 || jpgAtts[0].Filename != "image_1.jpg" || jpgAtts[1].Filename != "image_2.jpg" || jpgAtts[0].MimeType != "image/jpeg" {
		t.Fatalf("jpg attachments %+v, err %v", jpgAtts, err)
	}
}

func TestSubmitRefusesFormatsMuseDoesNotMake(t *testing.T) {
	p := testProvider(t, &fakePage{})
	for _, req := range []MuseJobRequest{
		{Kind: MuseJobVideo, Prompts: []string{"x"}, Ratio: "4:5"},
		{Kind: MuseJobImage, Prompts: []string{"x"}, Ratio: "2:1"},
		{Kind: MuseJobImage, Prompts: []string{"x"}, Duration: 5},
		{Kind: MuseJobVideo, Prompts: []string{"x"}, Duration: 15},
	} {
		if res := p.SubmitJobs(req); res.Success {
			t.Errorf("queued %+v", req)
		}
	}
	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"x"}, Ratio: "1:1", Duration: 10})
	if !res.Success || res.Data[0].Ratio != "1:1" || res.Data[0].Duration != 10 {
		t.Fatalf("submit: %+v", res)
	}
	opts := p.GetOptions().Data
	if len(opts.Ratios) == 0 || len(opts.Durations) == 0 {
		t.Fatalf("options offer no formats: %+v", opts)
	}
}

// What Muse made is checked against what was asked: its near-9:16 images are
// fine, a turned frame or a shorter video gets a note.
func TestFormatNote(t *testing.T) {
	image := MuseJob{Kind: MuseJobImage, Ratio: "9:16"}
	if note := formatNote(image, 1152, 1792, 0); note != "" {
		t.Errorf("Muse's closest 9:16 image noted: %q", note)
	}
	if note := formatNote(image, 1792, 1152, 0); !strings.Contains(note, "1792×1152") {
		t.Errorf("turned image not noted: %q", note)
	}
	video := MuseJob{Kind: MuseJobVideo, Ratio: "9:16", Duration: 10}
	if note := formatNote(video, 720, 1280, 10); note != "" {
		t.Errorf("exact video noted: %q", note)
	}
	if note := formatNote(MuseJob{Kind: MuseJobVideo, Duration: 8}, 720, 1280, 5); !strings.Contains(note, "5 giây thay vì 8") {
		t.Errorf("short video not noted: %q", note)
	}
	if note := formatNote(MuseJob{Kind: MuseJobImage}, 1, 9, 0); note != "" {
		t.Errorf("a job that asked nothing got %q", note)
	}
}

// The ratio and duration go out in the message, and the made file is measured.
func TestJobSendsFormatAndNotesTheResult(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Images: []ResultMedia{{Path: "/out/a.png", Mime: "image/png", ByteLen: int64(len(png16x9))}}}},
		files:  map[string][]byte{"/out/a.png": png16x9}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Prompts: []string{"x"}, Ratio: "9:16"}).Data[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	if len(page.sends) != 1 || page.sends[0] != "Task: Create a new image.\n"+requestRules+"\nAspect ratio: 9:16 (portrait)\nprompt:\nx" {
		t.Fatalf("sent %q", page.sends)
	}
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || !strings.Contains(got.FormatNote, "16×9") {
		t.Fatalf("job = %+v", got)
	}
}

// png16x9 is a 16×9 PNG.
var png16x9 = func() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 16, 9)))
	return buf.Bytes()
}()

// Editing a held job's prompt keeps it held; editing an ended one runs it.
func TestEditKeepsHoldOfWaitingJob(t *testing.T) {
	p := testProvider(t, &fakePage{})
	job := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Prompts: []string{"a"}, Held: true}).Data[0]
	if res := p.EditAndRetry(job.ID, "b"); !res.Success || !res.Data.Held || res.Data.Prompt != "b" {
		t.Fatalf("held edit = %+v", res)
	}
	p.updateJob(job.ID, func(j *MuseJob) { j.Status = MuseJobFailed })
	if res := p.EditAndRetry(job.ID, "c"); !res.Success || res.Data.Held || res.Data.Status != MuseJobQueued {
		t.Fatalf("failed edit = %+v", res)
	}
}

func TestUpdateJobsFormatUpdatesUnstartedQueueAndStoryboard(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"completed"}}
	page.events, page.files = videoResult("/v.mp4", []byte("v"))
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 6)
	outDir := t.TempDir()
	p.media.AllowRoot(outDir)

	// Queue a video batch with storyboard, a single video, an image batch, and a paid video.
	batchRes := p.SubmitJobs(MuseJobRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, Prompts: []string{"v1"}, OutputDir: outDir,
		Held: true, Ratio: "16:9", Duration: 10, Storyboard: &MuseStoryboard{Frames: 2},
	})
	if !batchRes.Success || len(batchRes.Data) != 2 {
		t.Fatalf("submit batch: %+v", batchRes)
	}
	board, video := batchRes.Data[0], batchRes.Data[1]
	paid := p.SubmitJobs(MuseJobRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, Prompts: []string{"paid"}, OutputDir: outDir,
		Held: true, Ratio: "16:9", Duration: 10,
	}).Data[0]
	p.updateJob(paid.ID, func(j *MuseJob) { j.SessionID = "already-sent" })
	single := p.SubmitJobs(MuseJobRequest{
		Kind: MuseJobVideo, Mode: MuseJobSingle, Prompts: []string{"single"},
		Held: true, Ratio: "16:9", Duration: 10,
	}).Data[0]
	imgBatch := p.SubmitJobs(MuseJobRequest{
		Kind: MuseJobImage, Mode: MuseJobBatch, Prompts: []string{"img"}, OutputDir: outDir,
		Held: true, Ratio: "16:9",
	}).Data[0]

	newRatio := "9:16"
	if res := p.UpdateJobsFormat(MuseJobsFormatRequest{Kind: MuseJobVideo, Mode: MuseJobBatch, Ratio: &newRatio}); !res.Success || res.Data != 2 {
		t.Fatalf("ratio update = %+v, want 2 (board + video)", res)
	}
	if got := mustJob(t, p, video.ID); got.Ratio != "9:16" || got.Duration != 10 {
		t.Fatalf("video after ratio update = %+v", got)
	}
	if got := mustJob(t, p, board.ID); got.Ratio != "9:16" || got.Duration != 0 {
		t.Fatalf("board after ratio update = %+v", got)
	}
	if got := mustJob(t, p, paid.ID); got.Ratio != "16:9" {
		t.Fatalf("paid job must stay untouched: %+v", got)
	}
	if got := mustJob(t, p, single.ID); got.Ratio != "16:9" {
		t.Fatalf("single job must stay untouched: %+v", got)
	}
	if got := mustJob(t, p, imgBatch.ID); got.Ratio != "16:9" {
		t.Fatalf("image batch job must stay untouched: %+v", got)
	}

	newDur := 30
	if res := p.UpdateJobsFormat(MuseJobsFormatRequest{Kind: MuseJobVideo, Mode: MuseJobBatch, Duration: &newDur}); !res.Success || res.Data != 1 {
		t.Fatalf("duration update = %+v, want 1 (video only)", res)
	}
	if got := mustJob(t, p, video.ID); got.Ratio != "9:16" || got.Duration != 30 {
		t.Fatalf("video after duration update = %+v", got)
	}
	if got := mustJob(t, p, board.ID); got.Duration != 0 {
		t.Fatalf("board image must keep duration 0: %+v", got)
	}

	// Clearing back to "Theo prompt" (empty ratio, 0 duration) also applies.
	emptyRatio, zeroDur := "", 0
	if res := p.UpdateJobsFormat(MuseJobsFormatRequest{Kind: MuseJobVideo, Mode: MuseJobBatch, Ratio: &emptyRatio, Duration: &zeroDur}); !res.Success || res.Data != 2 {
		t.Fatalf("clear format = %+v, want 2", res)
	}
	if got := mustJob(t, p, video.ID); got.Ratio != "" || got.Duration != 0 {
		t.Fatalf("video after clear = %+v", got)
	}
}

func TestUpdateJobsConfigMovesFolderAndUpdatesRefs(t *testing.T) {
	page := &fakePage{loggedIn: true}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 6)

	outA := t.TempDir()
	outB := t.TempDir()
	p.media.AllowRoot(outA)
	p.media.AllowRoot(outB)

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	ref1, err := p.media.Put("ref-1", "", ".png", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := p.media.Put("ref-2", "", ".png", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	vaultImg, err := p.media.Put("vault-hero", "", ".png", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	assetRes := p.SaveAsset(MuseAsset{Tag: "hero", Role: MuseRoleCharacter, Images: []string{vaultImg}})
	if !assetRes.Success {
		t.Fatalf("save asset: %+v", assetRes)
	}

	// Queue 2 prompts without manual refs into outA (prompt 1 uses @hero, prompt 2 pairs with storyboard).
	res1 := p.SubmitJobs(MuseJobRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, Prompts: []string{"@hero chạy bộ"}, OutputDir: outA, Held: true,
	})
	res2 := p.SubmitJobs(MuseJobRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, Prompts: []string{"mèo trên mái"}, OutputDir: outA, Held: true,
		Storyboard: &MuseStoryboard{Frames: 2},
	})
	paid := p.SubmitJobs(MuseJobRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, Prompts: []string{"paid"}, OutputDir: outA, Held: true,
	}).Data[0]
	p.updateJob(paid.ID, func(j *MuseJob) { j.SessionID = "s-paid" })
	single := p.SubmitJobs(MuseJobRequest{
		Kind: MuseJobVideo, Mode: MuseJobSingle, Prompts: []string{"single"}, Held: true,
	}).Data[0]
	if !res1.Success || !res2.Success || len(res2.Data) != 2 {
		t.Fatalf("submit = %+v / %+v", res1, res2)
	}
	v1, board2, v2 := res1.Data[0], res2.Data[0], res2.Data[1]

	// Selecting a new OutputDir after queueing moves unpaid batch jobs into outB with sequential indices.
	if up := p.UpdateJobsConfig(MuseJobsConfigRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, OutputDir: &outB,
	}); !up.Success || up.Data != 3 {
		t.Fatalf("outputDir update = %+v, want 3 (v1, board2, v2)", up)
	}
	gv1, gBoard2, gv2 := mustJob(t, p, v1.ID), mustJob(t, p, board2.ID), mustJob(t, p, v2.ID)
	if filepath.Dir(gv1.OutputDir) != outB || gv1.Index != 1 {
		t.Fatalf("v1 folder/index = %q #%d", gv1.OutputDir, gv1.Index)
	}
	if gv2.OutputDir != gv1.OutputDir || gv2.Index != 2 || gBoard2.OutputDir != gv2.OutputDir || gBoard2.Index != 2 {
		t.Fatalf("v2/board2 folder/index = %q #%d / %q #%d", gv2.OutputDir, gv2.Index, gBoard2.OutputDir, gBoard2.Index)
	}
	if gotPaid := mustJob(t, p, paid.ID); filepath.Dir(gotPaid.OutputDir) != outA {
		t.Fatalf("paid job must stay in outA: %q", gotPaid.OutputDir)
	}
	// Selecting the same parent directory again makes no redundant update.
	if up := p.UpdateJobsConfig(MuseJobsConfigRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, OutputDir: &outB,
	}); !up.Success || up.Data != 0 {
		t.Fatalf("same outputDir update = %+v, want 0", up)
	}

	// Picking references + pair mode after queueing preserves @hero and assigns paired refs cleanly.
	if up := p.UpdateJobsConfig(MuseJobsConfigRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, SyncRefs: true,
		References: []string{ref1, ref2}, RefMode: MuseRefPair,
	}); !up.Success || up.Data != 3 {
		t.Fatalf("refs update = %+v, want 3", up)
	}
	gv1, gBoard2, gv2 = mustJob(t, p, v1.ID), mustJob(t, p, board2.ID), mustJob(t, p, v2.ID)
	if len(gv1.References) != 2 || gv1.References[1] != ref1 || gv1.RefInfo[0].Tag != "hero" {
		t.Fatalf("v1 refs/info = %+v / %+v", gv1.References, gv1.RefInfo)
	}
	if !slices.Equal(gv2.References, []string{ref2}) || !slices.Equal(gBoard2.References, []string{ref2}) {
		t.Fatalf("v2/board2 refs = %+v / %+v, want [%s]", gv2.References, gBoard2.References, ref2)
	}
	if gotPaid := mustJob(t, p, paid.ID); len(gotPaid.References) != 0 {
		t.Fatalf("paid job refs mutated: %+v", gotPaid.References)
	}
	if gotSingle := mustJob(t, p, single.ID); len(gotSingle.References) != 0 {
		t.Fatalf("single job refs mutated: %+v", gotSingle.References)
	}

	// Clearing manual references keeps @hero on v1 and empties v2/board2.
	if up := p.UpdateJobsConfig(MuseJobsConfigRequest{
		Kind: MuseJobVideo, Mode: MuseJobBatch, SyncRefs: true, References: []string{},
	}); !up.Success || up.Data != 3 {
		t.Fatalf("clear refs = %+v, want 3", up)
	}
	gv1, gBoard2, gv2 = mustJob(t, p, v1.ID), mustJob(t, p, board2.ID), mustJob(t, p, v2.ID)
	if len(gv1.References) != 1 || gv1.RefInfo[0].Tag != "hero" {
		t.Fatalf("v1 should retain @hero only: %+v / %+v", gv1.References, gv1.RefInfo)
	}
	if len(gv2.References) != 0 || len(gBoard2.References) != 0 {
		t.Fatalf("v2/board2 should be empty: %+v / %+v", gv2.References, gBoard2.References)
	}
}
