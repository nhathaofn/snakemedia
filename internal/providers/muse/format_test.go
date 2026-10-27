package muse

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strconv"
	"strings"
	"testing"
)

func TestMessageAsksForRatioAndDuration(t *testing.T) {
	cases := []struct {
		job  MuseJob
		want string
	}{
		{MuseJob{Kind: MuseJobImage, Prompt: "p"}, "Task: Create a new image.\nPrompt:\np"},
		{MuseJob{Kind: MuseJobImage, Prompt: "p", Ratio: "3:4"},
			"Task: Create a new image.\nAspect ratio: 3:4 (portrait)\nPrompt:\np"},
		{MuseJob{Kind: MuseJobVideo, Prompt: "p", Ratio: "16:9", Duration: 8},
			"Task: Create a new video.\nAspect ratio: 16:9 (landscape)\nDuration: 8 seconds\nPrompt:\np"},
		{MuseJob{Kind: MuseJobVideo, Prompt: "p", Duration: 5}, "Task: Create a new video.\nDuration: 5 seconds\nPrompt:\np"},
		// An image never asks for a length, nor for a ratio it is not offered.
		{MuseJob{Kind: MuseJobImage, Prompt: "p", Duration: 5}, "Task: Create a new image.\nPrompt:\np"},
		// One attached image is the subject, kept as drawn; its role is the
		// prompt's to change, and the ratio asked wins over the image's shape.
		{MuseJob{Kind: MuseJobVideo, Prompt: "p", References: []string{"r"}, Ratio: "1:1", Duration: 5},
			"Task: Create a new video.\n" +
				"Reference: The attached image (image 1) shows the subject of this video; " +
				"keep its design, colors, details and art style exactly, consistent in every frame. " +
				"Do not copy the image's background, framing or pose; take those from the prompt. " +
				"If the prompt gives the image another role (for example the style only, or the opening frame), follow the prompt instead.\n" +
				"Aspect ratio: 1:1 (square). Frame the video in this ratio, not in the shape of the attached images.\n" +
				"Duration: 5 seconds\nPrompt:\np"},
	}
	for _, c := range cases {
		if got := buildMessage(c.job); got != c.want {
			t.Errorf("message %q, want %q", got, c.want)
		}
	}
}

// Several images are counted and numbered from the job itself, each a
// separate element unless the prompt says otherwise; nothing is fixed to a
// number of images.
func TestMessageNumbersEveryAttachedImage(t *testing.T) {
	for n, names := range map[int]string{2: "image 1 and image 2", 3: "image 1, image 2 and image 3", 7: "image 1 to image 7"} {
		refs := make([]string, n)
		msg := buildMessage(MuseJob{Kind: MuseJobImage, Prompt: "p", References: refs})
		for _, want := range []string{
			"Reference: " + strconv.Itoa(n) + " images are attached, numbered in the order attached (" + names + ").",
			"Do not merge them into one subject.",
			"include every element shown and keep each one's design, colors, details and art style exactly.",
			"its image overrides any description of it in the prompt.",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("%d images: message lacks %q:\n%s", n, want, msg)
			}
		}
		if strings.Contains(msg, "consistent in every frame") || strings.Contains(msg, "keyframe") {
			t.Errorf("%d images: image message speaks of frames:\n%s", n, msg)
		}
	}
}

// Attached files are named image_N, as the message numbers them.
func TestAttachmentsAreNamedAsNumbered(t *testing.T) {
	p := testProvider(t, &fakePage{})
	a, _ := p.media.Put("muse-ref-a", "image/png", ".png", []byte("A"))
	b, _ := p.media.Put("muse-ref-b", "image/png", ".png", []byte("B"))
	caps := &MuseCapabilities{MaxFiles: 10, MaxFileBytes: 1 << 20, ImageTypes: []MuseFileType{{Mime: "image/png", Extensions: []string{".png"}}}}
	atts, err := p.attach(MuseJob{References: []string{a, b}}, caps)
	if err != nil || len(atts) != 2 || atts[0].Filename != "image_1.png" || atts[1].Filename != "image_2.png" {
		t.Fatalf("attachments %+v, err %v", atts, err)
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

	if len(page.sends) != 1 || !strings.HasPrefix(page.sends[0], "Task: Create a new image.\nAspect ratio: 9:16 (portrait)\n") {
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
