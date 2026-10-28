package muse

import (
	"context"
	"slices"
	"strings"
	"testing"

	"SnakeMedia/internal/accounts"
)

// withCaps stores Muse's limits on the account so submits can check them.
func withCaps(p *Provider, acc accounts.Record, maxFiles int) {
	caps := testCaps()
	caps.MaxFiles = maxFiles
	p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.Capabilities = caps })
}

func saveAsset(t *testing.T, p *Provider, tag, name string, role MuseRefRole, lock string) MuseAsset {
	t.Helper()
	img, err := p.media.Put(assetPrefix+tag, "image/png", ".png", []byte(tag))
	if err != nil {
		t.Fatal(err)
	}
	res := p.SaveAsset(MuseAsset{Tag: tag, Name: name, Role: role, Lock: lock, Images: []string{img}})
	if !res.Success {
		t.Fatalf("save asset: %s", res.Error)
	}
	return res.Data
}

func TestMentionedAssetsFollowPromptOrderAndWholeWords(t *testing.T) {
	assets := []MuseAsset{{Tag: "ao", Name: "Áo xanh"}, {Tag: "mai", Name: "Mai"}, {Tag: "tom", Name: "Tom"}}
	got := mentionedAssets("Ngày mai, Mai mặc @AO đứng cạnh @tomcat", assets)
	tags := []string{}
	for _, a := range got {
		tags = append(tags, a.Tag)
	}
	if !slices.Equal(tags, []string{"mai", "ao"}) {
		t.Fatalf("tags = %v", tags)
	}
	if unk := unknownTags("cùng @AO và @lạ, email a@b", assets); !slices.Equal(unk, []string{"lạ"}) {
		t.Fatalf("unknown = %v", unk)
	}
}

func TestPromptRefsModes(t *testing.T) {
	own := []string{"a", "b"}
	cases := []struct {
		mode  MuseRefMode
		index int
		want  []string
	}{
		{MuseRefShared, 2, []string{"a", "b"}},
		{MuseRefPair, 1, []string{"b"}},
		{MuseRefPair, 2, nil},
		{MuseRefRotate, 3, []string{"b"}},
	}
	for _, c := range cases {
		refs, info := promptRefs("x", c.index, nil, nil, own, c.mode)
		if !slices.Equal(refs, c.want) || info != nil {
			t.Fatalf("%s/%d: refs %v info %v", c.mode, c.index, refs, info)
		}
	}
	fixed := []MuseAsset{{Tag: "model", Role: MuseRoleCharacter, Images: []string{"m"}}}
	refs, info := promptRefs("x", 0, fixed, nil, []string{"m", "c"}, MuseRefShared)
	if !slices.Equal(refs, []string{"m", "c"}) || len(info) != 2 || info[0].Role != MuseRoleCharacter || info[1].Tag != "" {
		t.Fatalf("refs %v info %+v", refs, info)
	}
}

func TestRoleLinesNameRolesPriorityAndLocks(t *testing.T) {
	job := MuseJob{Kind: MuseJobVideo, Prompt: "p", References: []string{"1", "2", "3"}, RefInfo: []MuseRefInfo{
		{Tag: "lan", Role: MuseRoleFace, Lock: "nốt ruồi má trái"}, {Tag: "hop", Role: MuseRoleProduct, Lock: "chữ ABC"}, {Tag: "sb", Role: MuseRoleStoryboard},
	}}
	msg := buildMessage(job)
	for _, want := range []string{
		"image 1 = @lan (character face)", "image 2 = @hop (product)", "image 3 = @sb (storyboard)",
		"- Product: @hop > storyboard", "- Identity: @lan", "Ignore any product drawn in it",
		"- @lan: nốt ruồi má trái", "- @hop: chữ ABC",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Reference: ") {
		t.Fatal("plain reference lines kept next to the roles")
	}
}

func TestSubmitAttachesNamedAssetsWithRoles(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"completed"}}
	page.events, page.files = videoResult("/v.mp4", []byte("v"))
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)
	lan := saveAsset(t, p, "lan", "Lan", MuseRoleFace, "")
	saveAsset(t, p, "hop", "", MuseRoleProduct, "")

	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"@hop trên bàn, Lan cầm lên", "không ai cả"}})
	if !res.Success {
		t.Fatal(res.Error)
	}
	first, second := res.Data[0], res.Data[1]
	if len(first.References) != 2 || first.RefInfo[0].Tag != "hop" || first.RefInfo[1].Tag != "lan" {
		t.Fatalf("first = %+v", first)
	}
	if len(second.References) != 0 || second.RefInfo != nil {
		t.Fatalf("second = %+v", second)
	}
	check := p.CheckPrompt("Lan và @meo", []string{lan.ID}).Data
	if check.Images != 1 || len(check.Warnings) != 2 {
		t.Fatalf("check = %+v", check)
	}

	p.runOne(context.Background(), first.ID, acc.ID)
	if got := mustJob(t, p, first.ID); got.Status != MuseJobSucceeded {
		t.Fatalf("job = %+v", got)
	}
	if !strings.Contains(page.sends[0], "Reference roles:") || len(page.attachments[0]) != 2 {
		t.Fatalf("sent %q with %d files", page.sends[0], len(page.attachments[0]))
	}
}

func TestStoryboardVideoWaitsForItsFrames(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"completed"},
		events: []AssistantEvent{{Images: []ResultMedia{{Path: "/1.png", Mime: "image/png", ByteLen: 1}, {Path: "/2.png", Mime: "image/png", ByteLen: 1}}}},
		files:  map[string][]byte{"/1.png": {1}, "/2.png": {2}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)

	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"hai cảnh"}, Storyboard: &MuseStoryboard{Frames: 2}})
	if !res.Success || len(res.Data) != 2 {
		t.Fatalf("submit = %+v", res)
	}
	board, video := res.Data[0], res.Data[1]
	if board.Kind != MuseJobImage || board.BoardFrames != 2 || video.After != board.ID {
		t.Fatalf("board %+v video %+v", board, video)
	}
	views := []MuseAccount{museView(mustRecord(t, p, acc.ID))}
	planned := plan(p.jobs.list(), views, p.cfg.Get().Muse, map[string]string{}, p.now())
	if len(planned) != 1 || planned[0].jobID != board.ID {
		t.Fatalf("planned %+v", planned)
	}

	p.runOne(context.Background(), board.ID, acc.ID)
	if !strings.HasPrefix(page.sends[0], "Task: Create the storyboard images of a video.") {
		t.Fatalf("board message %q", page.sends[0])
	}
	got := mustJob(t, p, video.ID)
	if len(got.Frames) != 2 {
		t.Fatalf("video = %+v", got)
	}

	page.events, page.files = videoResult("/v.mp4", []byte("v"))
	page.statuses = []string{"completed"}
	p.runOne(context.Background(), video.ID, acc.ID)
	if got := mustJob(t, p, video.ID); got.Status != MuseJobSucceeded {
		t.Fatalf("video = %+v", got)
	}
	if !strings.Contains(page.sends[1], "Storyboard frames: image 1 to image 2") || len(page.attachments[1]) != 2 {
		t.Fatalf("video message %q, %d files", page.sends[1], len(page.attachments[1]))
	}
}

func TestStoryboardReviewHoldsTheVideo(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)
	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"x"}, Storyboard: &MuseStoryboard{Frames: 1, Review: true}})
	if !res.Success || res.Data[0].Held || !res.Data[1].Held {
		t.Fatalf("submit = %+v", res)
	}
	if res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Prompts: []string{"x"}, Storyboard: &MuseStoryboard{Frames: 1}}); res.Success {
		t.Fatal("storyboard accepted for an image")
	}
}

func mustRecord(t *testing.T, p *Provider, id string) accounts.Record {
	t.Helper()
	rec, err := p.accounts.Record(id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// A video that already has its frames no longer needs the storyboard job,
// which "clear finished" may remove; under review it is held again when the
// frames arrive, even if the whole batch was run before.
func TestStoryboardVideoSurvivesClearedBoardAndHoldsForReview(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"completed"},
		events: []AssistantEvent{{Images: []ResultMedia{{Path: "/1.png", Mime: "image/png", ByteLen: 1}}}},
		files:  map[string][]byte{"/1.png": {1}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)
	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"x"}, Held: true, Storyboard: &MuseStoryboard{Frames: 1, Review: true}})
	if !res.Success {
		t.Fatal(res.Error)
	}
	board, video := res.Data[0], res.Data[1]
	p.RunJobs([]string{board.ID, video.ID}) // "Chạy" on the whole batch

	p.runOne(context.Background(), board.ID, acc.ID)
	got := mustJob(t, p, video.ID)
	if len(got.Frames) != 1 || !got.Held {
		t.Fatalf("video after board = %+v", got)
	}
	p.ClearFinished(MuseJobVideo, MuseJobBatch)
	if _, ok := p.jobs.get(board.ID); ok {
		t.Fatal("board job not cleared")
	}
	p.RunJobs([]string{video.ID})
	views := []MuseAccount{museView(mustRecord(t, p, acc.ID))}
	planned := plan(p.jobs.list(), views, p.cfg.Get().Muse, map[string]string{}, p.now())
	if len(planned) != 1 || planned[0].jobID != video.ID {
		t.Fatalf("planned %+v", planned)
	}
}

func TestMentionOrderUsesPromptOffsets(t *testing.T) {
	assets := []MuseAsset{{Tag: "b", Name: "Bình"}, {Tag: "a"}}
	// "İ" lower-cases to more bytes; offsets must still come from the prompt.
	got := mentionedAssets("İİİİ @A rồi Bình", assets)
	if len(got) != 2 || got[0].Tag != "a" || got[1].Tag != "b" {
		t.Fatalf("got %+v", got)
	}
}
