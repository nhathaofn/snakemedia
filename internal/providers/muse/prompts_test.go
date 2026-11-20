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
			if !strings.HasSuffix(msg, "\nprompt:\n"+prompt) {
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

func TestFitRuleAlwaysGeneratesVideo(t *testing.T) {
	job := MuseJob{Kind: MuseJobVideo, Duration: 5, Prompt: "a character talking"}
	msg := buildMessage(job)
	want := "Always generate the video, even if a line feels long for the clip: start the line as early as possible, keep a natural pace, and let any remaining time be a silent hold on the ending state. Never refuse, and never return text instead of a video."
	if !strings.Contains(msg, want) {
		t.Fatalf("expected new fit rule in message, got: %s", msg)
	}
	if strings.Contains(msg, "make nothing and say how long is needed") {
		t.Fatalf("old fit rule still present in message: %s", msg)
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
		if strings.Count(msg, wardrobeExceptionRule) != 1 {
			t.Fatal("wardrobe exception rule missing or duplicated")
		}
		if !strings.Contains(msg, wardrobeExceptionRule+"\n"+referenceSheetRule) {
			t.Fatal("wardrobe exception rule must precede reference sheet rule")
		}
		if !strings.HasSuffix(msg, "\nprompt:\n"+job.Prompt) {
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

func TestOverallBatchVideoSystemPrompt(t *testing.T) {
	job := MuseJob{
		Kind:       MuseJobVideo,
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
Use only this message and its attached images; ignore earlier chats and outputs unless this message asks to reuse them.
Reference roles: these attached images are references for this video, numbered in the order attached:
- image 1 = @alias_dia_diem (setting): the place: keep its layout, materials, light and colors. Do not take people or products from it.
- image 2 = @alias_nhan_vat (character, full look): character authority: keep this exact face, body proportions, hair and outfit whenever this character is visible.
- image 3 = @alias_dao_cu (prop): keep this object's exact shape, material and colors whenever it is visible. Do not take people or backgrounds from it.
Images with the same @name show one character or object from different views: they are one subject, not several.
Priority when they disagree:
- Identity: @alias_nhan_vat > faces in a storyboard > prompt text. The same person looks the same in every shot.
Apply each role only to its subject and attributes; the prompt decides actions and lines, and may give a role to an image without one. Do not copy backgrounds, framing or poses outside an image's role, or put its subject in shots that do not call for it.
Wardrobe exception: locks and reference images govern identity only - face, hair, body, and for settings the layout, materials and light. Whenever the prompt contains a "Wardrobe this shot" line, that line overrides any outfit named in a lock or shown in a reference image.
Reference images are guidance, never frames: never show a reference sheet, its grid, multiple views or plain background, even at the start or end; frame 0 already shows the scene.
The prompt is the plan: keep its subjects, action order, camera, style and every quoted line word for word, in its language, by its speaker; speaker names are labels, not words to say. Fill in only what it leaves open. Add no other characters, events or speech, and no captions, subtitles, watermarks or music it does not ask for.
Always generate the video, even if a line feels long for the clip: start the line as early as possible, keep a natural pace, and let any remaining time be a silent hold on the ending state. Never refuse, and never return text instead of a video.
prompt:
{{PROMPT}}`
	if msg != want {
		t.Fatalf("buildMessage diff:\nGOT:\n%s\n\nWANT:\n%s", msg, want)
	}
}
