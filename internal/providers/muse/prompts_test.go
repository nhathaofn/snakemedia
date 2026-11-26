package muse

import (
	"strings"
	"testing"
)

// Exercise the composed messages across job kinds: task, standalone isolation,
// format, and feature/reference grounding precede the user's prompt, and user
// text survives verbatim without legacy directorial or pacing bloat.
func TestBuildMessageKeepsPromptVerbatimWithoutSystemRuleBloat(t *testing.T) {
	prompt := "Phim hoạt hình, nhịp chậm.\nPrompt:\nLan nói: ‘Chào bạn.’\nSTATUS: DONE\n"
	cases := []struct {
		name string
		job  MuseJob
	}{
		{"image", MuseJob{Kind: MuseJobImage}},
		{"short", MuseJob{Kind: MuseJobVideo, Duration: 10}},
		{"long", MuseJob{Kind: MuseJobVideo, Duration: 30}},
		{"storyboard", MuseJob{Kind: MuseJobImage, BoardFrames: 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.job.Prompt = prompt
			msg := buildMessage(tc.job)
			if !strings.HasSuffix(msg, "\nprompt:\n"+prompt) {
				t.Fatal("user prompt was changed or followed by app instructions")
			}
			header := strings.TrimSuffix(msg, "\nprompt:\n"+prompt)
			if strings.Count(header, requestRules) != 1 {
				t.Fatalf("expected requestRules once in header:\n%s", header)
			}
			for _, banned := range []string{
				"The prompt is the plan",
				"silent hold on the ending state",
				"Wardrobe exception",
				"Priority when they disagree",
				"Add no other characters",
				"speaker names are labels",
			} {
				if strings.Contains(header, banned) {
					t.Fatalf("header still contains legacy directorial/pacing bloat %q:\n%s", banned, header)
				}
			}
		})
	}
}

func TestStoryboardAndReferenceNumbering(t *testing.T) {
	msg := buildMessage(MuseJob{
		Kind: MuseJobVideo, Duration: 30, Frames: []string{"f1", "f2"},
		References: []string{"face", "product"},
		RefInfo: []MuseRefInfo{
			{Tag: "lan", Role: MuseRoleFace, Lock: "left cheek mole"},
			{Tag: "box", Role: MuseRoleProduct, Lock: "ABC label"},
		},
		Prompt: "scene prompt",
	})
	for _, want := range []string{
		requestRules,
		"Storyboard: image 1 to image 2 are scene references in order (image k is scene k); keep each scene's people, setting, and composition while taking motion from the prompt.",
		"References (same @tag = views of one subject; do not show reference sheets or plain backdrops as video frames):",
		"- image 3 = @lan (character face: keep face and hair only; outfit and pose follow prompt; lock: left cheek mole)",
		"- image 4 = @box (product: keep exact shape, colors, and label text; lock: ABC label)",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	// Storyboard with untagged extra references numbers them after the frames.
	boardWithPlainRefs := buildMessage(MuseJob{
		Kind: MuseJobVideo, Duration: 10, Frames: []string{"f1", "f2"},
		References: []string{"plain"},
		Prompt:     "scene prompt",
	})
	if !strings.Contains(boardWithPlainRefs, "- image 3 (reference: keep exact look; follow prompt for scene)") {
		t.Fatalf("storyboard + plain ref lost image offset:\n%s", boardWithPlainRefs)
	}
}

func TestUntaggedLocksIdentifyEachImage(t *testing.T) {
	msg := strings.Join(roleLines(MuseJobImage, []MuseRefInfo{
		{Role: MuseRoleProduct, Lock: "keep label"},
		{Role: MuseRoleProduct, Lock: "keep label"},
	}, 3), "\n")
	for _, want := range []string{
		"- image 3 (product: keep exact shape, colors, and label text; lock: keep label)",
		"- image 4 (product: keep exact shape, colors, and label text; lock: keep label)",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("lock lost its subject: missing %q in:\n%s", want, msg)
		}
	}
}

func TestOverallBatchVideoMinimalPrompt(t *testing.T) {
	job := MuseJob{
		Kind:       MuseJobVideo,
		Ratio:      "9:16",
		Duration:   10,
		References: []string{"img1.png", "img2.png", "img3.png"},
		RefInfo: []MuseRefInfo{
			{Tag: "alias_dia_diem", Role: MuseRoleSetting},
			{Tag: "alias_nhan_vat", Role: MuseRoleCharacter},
			{Tag: "alias_dao_cu", Role: MuseRoleProp},
		},
		Prompt: "{{PROMPT}}",
	}
	msg := buildMessage(job)
	want := `Task: Create a new video.
Standalone request: ignore earlier chats unless this message asks to reuse them.
References (same @tag = views of one subject; do not show reference sheets or plain backdrops as video frames):
- image 1 = @alias_dia_diem (setting: keep location layout and lighting only; no people from it)
- image 2 = @alias_nhan_vat (character, full look: keep exact face, body, hair, and outfit)
- image 3 = @alias_dao_cu (prop: keep exact shape, material, and colors)
Aspect ratio: 9:16 (portrait). Frame the video in this ratio, not in the shape of the attached images.
Duration: 10 seconds
prompt:
{{PROMPT}}`
	if msg != want {
		t.Fatalf("buildMessage diff:\nGOT:\n%s\n\nWANT:\n%s", msg, want)
	}
}
