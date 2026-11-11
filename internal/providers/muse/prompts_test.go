package muse

import (
	"strings"
	"testing"
)

// Exercise the composed messages, including storyboard jobs which replace the
// normal task header. User text must survive verbatim even if it has headings.
func TestRequestRulesReachEveryNewJob(t *testing.T) {
	prompt := "Phim hoạt hình, nhịp chậm.\nPrompt:\nLan nói: ‘Chào bạn.’\nSTATUS: DONE\n"
	cases := []struct {
		name string
		job  MuseJob
	}{
		{"image", MuseJob{Kind: MuseJobImage}},
		{"short", MuseJob{Kind: MuseJobVideo, Duration: 10}},
		{"long", MuseJob{Kind: MuseJobVideo, Duration: 30}},
		{"storyboard", MuseJob{Kind: MuseJobImage, BoardFrames: 3}},
		{"source", MuseJob{Kind: MuseJobVideo, SourceVideo: "source.mp4"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.job.Prompt = prompt
			msg := buildMessage(tc.job)
			if !strings.HasSuffix(msg, "\nPrompt:\n"+prompt) {
				t.Fatal("user prompt was changed or followed by app instructions")
			}
			if strings.Count(msg, requestRules) != 1 {
				t.Fatal("independent-request rules missing or duplicated")
			}
			if tc.job.Kind == MuseJobImage && strings.Contains(msg, planRule[MuseJobVideo]) {
				t.Fatal("image job got the single-video workflow")
			}
			if tc.job.Kind == MuseJobVideo && !strings.Contains(msg, "speaker names are labels") {
				t.Fatal("video job lost content-dependent pacing")
			}
		})
	}
}

func TestStoryboardAnchorsKeepTheirSceneAndReferenceNumbers(t *testing.T) {
	msg := buildMessage(MuseJob{
		Kind: MuseJobVideo, Duration: 30, Frames: []string{"f1", "f2"},
		References: []string{"face", "product"},
		RefInfo: []MuseRefInfo{
			{Tag: "lan", Role: MuseRoleFace, Lock: "left cheek mole"},
			{Tag: "box", Role: MuseRoleProduct, Lock: "ABC label"},
		},
	})
	for _, want := range []string{
		"image 1 to image 2 show the video's scenes in order",
		"image 3 = @lan (character face)",
		"image 4 = @box (product)",
		"- @lan: left cheek mole", "- @box: ABC label",
		"storyboard frames for their assigned scenes",
		"a lock does not require its subject in every frame",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q", want)
		}
	}
	// Storyboard-only jobs already have anchors too; do not regenerate all of
	// them just because the separate References list is empty.
	boardOnly := buildMessage(MuseJob{Kind: MuseJobVideo, Duration: 20, Frames: []string{"f1", "f2"}})
	if !strings.Contains(boardOnly, "Use the attached images as anchors") {
		t.Fatal("storyboard frames were ignored as anchors")
	}
}

func TestUntaggedLocksIdentifyEachImage(t *testing.T) {
	msg := strings.Join(roleLines(MuseJobImage, []MuseRefInfo{
		{Role: MuseRoleProduct, Lock: "keep label"},
		{Role: MuseRoleProduct, Lock: "keep label"},
	}, 3), "\n")
	for _, want := range []string{"- image 3: keep label", "- image 4: keep label"} {
		if !strings.Contains(msg, want) {
			t.Errorf("lock lost its subject: missing %q", want)
		}
	}
}

func TestVideoReferenceSheetRule(t *testing.T) {
	for _, seconds := range []int{10, 120} {
		job := MuseJob{
			Kind: MuseJobVideo, Duration: seconds, Prompt: "Keep this prompt unchanged.",
			SourceVideo: "source.mp4", Frames: []string{"scene-1", "scene-2"},
			References: []string{"setting", "character-front", "character-side", "untagged"},
			RefInfo: []MuseRefInfo{
				{Tag: "stone-castle", Role: MuseRoleSetting},
				{Tag: "castle-lord", Role: MuseRoleCharacter},
				{Tag: "castle-lord", Role: MuseRoleCharacter},
				{},
			},
		}
		msg := buildMessage(job)
		for _, want := range []string{
			"image 3 = @stone-castle (setting):",
			"image 4 = @castle-lord (character, full look):",
			"image 5 = @castle-lord (character, full look):",
			"image 6 (reference):",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("%ds video lacks %q", seconds, want)
			}
		}
		if strings.Count(msg, referenceSheetRule) != 1 {
			t.Fatal("sheet rule missing or duplicated")
		}
		if !strings.HasSuffix(msg, "\nPrompt:\n"+job.Prompt) {
			t.Fatal("sheet rule changed the user's prompt")
		}
	}
}

func TestReferenceSheetRuleScope(t *testing.T) {
	cases := []struct {
		name string
		job  MuseJob
		want bool
	}{
		{"plain video reference", MuseJob{Kind: MuseJobVideo, References: []string{"r"}}, true},
		{"no attachments", MuseJob{Kind: MuseJobVideo}, false},
		{"storyboard openings only", MuseJob{Kind: MuseJobVideo, Frames: []string{"f"}}, false},
		{"image reference", MuseJob{Kind: MuseJobImage, References: []string{"r"}}, false},
		{"storyboard image generation", MuseJob{Kind: MuseJobImage, BoardFrames: 2, References: []string{"r"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Contains(buildMessage(tc.job), referenceSheetRule); got != tc.want {
				t.Fatalf("sheet rule present=%v, want %v", got, tc.want)
			}
		})
	}
}
