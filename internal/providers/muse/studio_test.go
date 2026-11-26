package muse

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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
	img, err := p.media.Put(assetPrefix+tag, "image/png", ".png", testPNGBytes(t, 16, 16))
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

func TestRoleLinesNameRolesAndLocksConcisely(t *testing.T) {
	job := MuseJob{Kind: MuseJobVideo, Prompt: "p", References: []string{"1", "2", "3"}, RefInfo: []MuseRefInfo{
		{Tag: "lan", Role: MuseRoleFace, Lock: "nốt ruồi má trái"}, {Tag: "hop", Role: MuseRoleProduct, Lock: "chữ ABC"}, {Tag: "sb", Role: MuseRoleStoryboard},
	}}
	msg := buildMessage(job)
	for _, want := range []string{
		"References (same @tag = views of one subject; do not show reference sheets or plain backdrops as video frames):",
		"- image 1 = @lan (character face: keep face and hair only; outfit and pose follow prompt; lock: nốt ruồi má trái)",
		"- image 2 = @hop (product: keep exact shape, colors, and label text; lock: chữ ABC)",
		"- image 3 = @sb (storyboard: follow framing, setting, and shot composition)",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Priority when they disagree") {
		t.Fatalf("verbose rules kept in message:\n%s", msg)
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
	// Section 13 ordering puts CHAR (@lan) before PROP (@hop) regardless of prompt order.
	if len(first.References) != 2 || first.RefInfo[0].Tag != "lan" || first.RefInfo[1].Tag != "hop" {
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
	if !strings.Contains(page.sends[0], "- image 1 = @lan (character face: keep face and hair only; outfit and pose follow prompt)\n- image 2 = @hop (product: keep exact shape, colors, and label text)") || len(page.attachments[0]) != 2 {
		t.Fatalf("sent %q with %d files", page.sends[0], len(page.attachments[0]))
	}
	if page.attachments[0][0].MimeType != "image/jpeg" || page.attachments[0][1].MimeType != "image/jpeg" {
		t.Fatalf("expected outgoing attachments normalized to JPEG: %+v", page.attachments[0])
	}
}

func TestStoryboardVideoWaitsForItsFrames(t *testing.T) {
	sbBytes := testPNGBytes(t, 16, 16)
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"completed"},
		events: []AssistantEvent{{Images: []ResultMedia{{Path: "/1.png", Mime: "image/png", ByteLen: int64(len(sbBytes))}, {Path: "/2.png", Mime: "image/png", ByteLen: int64(len(sbBytes))}}}},
		files:  map[string][]byte{"/1.png": sbBytes, "/2.png": sbBytes}}
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
	if !strings.Contains(page.sends[1], "Storyboard: image 1 to image 2 are scene references in order") || len(page.attachments[1]) != 2 {
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
	sbBytes := testPNGBytes(t, 16, 16)
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"completed"},
		events: []AssistantEvent{{Images: []ResultMedia{{Path: "/1.png", Mime: "image/png", ByteLen: int64(len(sbBytes))}}}},
		files:  map[string][]byte{"/1.png": sbBytes}}
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

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanAssetFolderSortsNaturallyAndFiltersNonImages(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)

	dir := t.TempDir()
	for _, name := range []string{"10.png", "2.png", "1.png", "nv-1.png", "nv.png", "notes.txt"} {
		pPath := filepath.Join(dir, name)
		if strings.HasSuffix(name, ".png") {
			writeTestPNG(t, pPath)
		} else if err := os.WriteFile(pPath, []byte("ignored"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Create an oversized image to verify error flagging without failing the scan.
	if err := os.WriteFile(filepath.Join(dir, "11_huge.png"), make([]byte, 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}

	res := p.ScanAssetFolder(context.Background(), dir, false)
	if !res.Success {
		t.Fatalf("scan failed: %s", res.Error)
	}
	var names []string
	for _, img := range res.Data.Images {
		names = append(names, img.FileName)
	}
	want := []string{"1.png", "2.png", "10.png", "11_huge.png", "nv.png", "nv-1.png"}
	if !slices.Equal(names, want) {
		t.Fatalf("order = %v, want %v", names, want)
	}
	if !strings.HasPrefix(res.Data.Images[0].PreviewURL, "/import-preview/") {
		t.Fatalf("expected lazy preview URL, got %q", res.Data.Images[0].PreviewURL)
	}
	if res.Data.Images[3].Error == "" || res.Data.Images[3].PreviewURL != "" {
		t.Fatalf("oversized image should flag error and skip preview: %+v", res.Data.Images[3])
	}
}

func TestAssetFolderScanHonorsCancellation(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "selected.png"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, recursive := range []bool{false, true} {
		res := p.ScanAssetFolder(ctx, dir, recursive)
		if !res.Success || !res.Data.Canceled || len(res.Data.Images) != 0 {
			t.Fatalf("expected a cancelled scan: %+v", res)
		}
	}
}

func TestImportAssetBatchConflictModesAndImmediateCleanup(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)

	srcDir := t.TempDir()
	imgA := filepath.Join(srcDir, "1.png")
	imgB := filepath.Join(srcDir, "2.png")
	imgC := filepath.Join(srcDir, "3.png")
	writeTestPNG(t, imgA)
	writeTestPNG(t, imgB)
	writeTestPNG(t, imgC)

	first := p.ImportAssetBatch(MuseAssetBatchRequest{
		Conflict: MuseConflictSkip,
		Items: []MuseAssetBatchItem{
			{SourcePath: imgA, Tag: "lan", Name: "Lan", Role: MuseRoleFace, Lock: "mặt"},
			{SourcePath: imgB, Tag: "lan", Name: "Lan dup", Role: MuseRoleFace},
			{SourcePath: imgC, Tag: "hop", Name: "Hộp", Role: MuseRoleProduct, Lock: "chữ ABC"},
		},
	})
	if !first.Success || first.Data.Added != 2 || first.Data.Skipped != 1 {
		t.Fatalf("first batch = %+v", first)
	}
	assets := p.assets.List()
	oldLanImg := assets[0].Images[0]
	oldHopImg := assets[1].Images[0]

	// Overwrite @lan (not used by any job): old image should be deleted immediately.
	over := p.ImportAssetBatch(MuseAssetBatchRequest{
		Conflict: MuseConflictOverwrite,
		Items: []MuseAssetBatchItem{
			{SourcePath: imgB, Tag: "LAN", Name: "Lan mới", Role: MuseRoleCharacter, Lock: "áo đỏ"},
		},
	})
	if !over.Success || over.Data.Updated != 1 || over.Data.Added != 0 {
		t.Fatalf("overwrite = %+v", over)
	}
	oldLanPath, _ := p.media.OpenPath(oldLanImg)
	if _, err := os.Stat(oldLanPath); !os.IsNotExist(err) {
		t.Fatalf("overwritten asset image was not deleted immediately: %v", err)
	}
	updatedLan, _ := p.assets.Get(assets[0].ID)
	if updatedLan.Role != MuseRoleCharacter || updatedLan.Lock != "áo đỏ" || updatedLan.Images[0] == oldLanImg {
		t.Fatalf("overwritten asset = %+v", updatedLan)
	}

	// Rename mode appends _2 when tag is already taken.
	ren := p.ImportAssetBatch(MuseAssetBatchRequest{
		Conflict: MuseConflictRename,
		Items:    []MuseAssetBatchItem{{SourcePath: imgC, Tag: "lan", Role: MuseRoleFace}},
	})
	if !ren.Success || ren.Data.Added != 1 {
		t.Fatalf("rename = %+v", ren)
	}
	renamed, _ := p.assets.Get(ren.Data.SavedIDs[0])
	if renamed.Tag != "lan_2" {
		t.Fatalf("renamed tag = %q, want lan_2", renamed.Tag)
	}

	// UpdateAssetsRole updates multiple assets in one call.
	upd := p.UpdateAssetsRole([]string{assets[0].ID, renamed.ID}, MuseRoleSetting)
	if !upd.Success || upd.Data != 2 {
		t.Fatalf("UpdateAssetsRole = %+v", upd)
	}

	// Queue a job referencing oldHopImg so RemoveAssets keeps that file while deleting unused ones.
	if _, err := p.jobs.add(MuseJob{ID: "j1", Kind: MuseJobImage, Status: MuseJobQueued, References: []string{oldHopImg}}); err != nil {
		t.Fatal(err)
	}
	rem := p.RemoveAssets([]string{assets[0].ID, assets[1].ID, renamed.ID})
	if !rem.Success || rem.Data != 3 {
		t.Fatalf("RemoveAssets = %+v", rem)
	}
	hopPath, _ := p.media.OpenPath(oldHopImg)
	if _, err := os.Stat(hopPath); err != nil {
		t.Fatalf("job-referenced asset image must stay on disk: %v", err)
	}
	newLanPath, _ := p.media.OpenPath(updatedLan.Images[0])
	if _, err := os.Stat(newLanPath); !os.IsNotExist(err) {
		t.Fatalf("removed asset image must be deleted immediately: %v", err)
	}
}

func TestPreviewMessagesBuildsWithoutWritingJournal(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)
	saveAsset(t, p, "lan", "Lan", MuseRoleFace, "nốt ruồi")

	res := p.PreviewMessages(MuseJobRequest{
		Kind:       MuseJobVideo,
		Prompts:    []string{"@lan bước qua cửa", "   ", "@unknown ở quán"},
		Ratio:      "9:16",
		Duration:   10,
		Storyboard: &MuseStoryboard{Frames: 2},
	})
	if !res.Success || len(res.Data) != 2 {
		t.Fatalf("PreviewMessages = %+v", res)
	}
	first, second := res.Data[0], res.Data[1]
	if first.Images != 1 || !slices.Equal(first.Tags, []string{"lan"}) {
		t.Fatalf("first preview meta = %+v", first)
	}
	if !strings.Contains(first.StoryboardMessage, "Task: Create the storyboard images of a video.") {
		t.Fatalf("expected StoryboardMessage, got %q", first.StoryboardMessage)
	}
	if !strings.Contains(first.Message, "Storyboard: image 1 to image 2") ||
		!strings.Contains(first.Message, "- image 3 = @lan") ||
		!strings.Contains(first.Message, "Aspect ratio: 9:16 (portrait)") ||
		!strings.Contains(first.Message, "Duration: 10 seconds") {
		t.Fatalf("first preview message mismatch:\n%s", first.Message)
	}
	if len(second.Warnings) == 0 || !strings.Contains(second.Warnings[0], "@unknown") {
		t.Fatalf("expected unknown tag warning on second prompt, got %+v", second.Warnings)
	}
	if len(p.jobs.list()) != 0 {
		t.Fatalf("PreviewMessages must write nothing to job journal, got %d jobs", len(p.jobs.list()))
	}
}

func TestCheckPromptForSpecSeparatesImageAndStoryboardSizes(t *testing.T) {
	p := testProvider(t, &fakePage{})
	saveAsset(t, p, "lan", "Lan", MuseRoleFace, "nốt ruồi")

	imgCheck := p.CheckPromptForSpec("@lan giữa phố", "", nil, MuseJobImage, 4)
	vidCheck := p.CheckPromptForSpec("@lan giữa phố", "", nil, MuseJobVideo, 0)
	sbCheck := p.CheckPromptForSpec("@lan giữa phố", "", nil, MuseJobVideo, 3)
	if !imgCheck.Success || !vidCheck.Success || !sbCheck.Success {
		t.Fatalf("check failed: img=%+v vid=%+v sb=%+v", imgCheck, vidCheck, sbCheck)
	}
	if imgCheck.Data.EstimatedBytes == vidCheck.Data.EstimatedBytes {
		t.Fatalf("expected image check bytes (%d) to differ from video (%d) and ignore boardFrames", imgCheck.Data.EstimatedBytes, vidCheck.Data.EstimatedBytes)
	}
	if sbCheck.Data.EstimatedBytes <= vidCheck.Data.EstimatedBytes {
		t.Fatalf("expected storyboard check bytes (%d) > plain video bytes (%d)", sbCheck.Data.EstimatedBytes, vidCheck.Data.EstimatedBytes)
	}
	if sbCheck.Data.Images != 1 {
		t.Fatalf("expected asset reference count 1 (not counting unmade storyboard frames), got %d", sbCheck.Data.Images)
	}
}

func TestImportAssetBatchPreservesItemLockAcrossModes(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 5)

	dir := t.TempDir()
	f1 := filepath.Join(dir, "a1.png")
	f2 := filepath.Join(dir, "a2.png")
	f3 := filepath.Join(dir, "b1.png")
	writeTestPNG(t, f1)
	writeTestPNG(t, f2)
	writeTestPNG(t, f3)

	mergedRes := p.ImportAssetBatch(MuseAssetBatchRequest{
		MergeByAlias: true,
		Conflict:     MuseConflictSkip,
		Items: []MuseAssetBatchItem{
			{SourcePath: f1, Tag: "hero", Role: MuseRoleCharacter, Lock: "scar on cheek", Angle: "front"},
			{SourcePath: f2, Tag: "hero", Role: MuseRoleCharacter, Lock: "red jacket", Angle: "side"},
		},
	})
	if !mergedRes.Success || len(mergedRes.Data.SavedIDs) != 1 {
		t.Fatalf("merged import failed: %+v", mergedRes)
	}
	hero, ok := p.assets.Get(mergedRes.Data.SavedIDs[0])
	if !ok || len(hero.Items) != 2 || hero.Items[0].Lock != "scar on cheek" || hero.Items[1].Lock != "red jacket" {
		t.Fatalf("merged item locks missing: %+v", hero.Items)
	}

	separateRes := p.ImportAssetBatch(MuseAssetBatchRequest{
		MergeByAlias: false,
		Conflict:     MuseConflictOverwrite,
		Items: []MuseAssetBatchItem{
			{SourcePath: f3, Tag: "villain", Role: MuseRoleCharacter, Lock: "gold watch", Angle: "front"},
		},
	})
	if !separateRes.Success || len(separateRes.Data.SavedIDs) != 1 {
		t.Fatalf("separate import failed: %+v", separateRes)
	}
	villain, ok := p.assets.Get(separateRes.Data.SavedIDs[0])
	if !ok || len(villain.Items) != 1 || villain.Items[0].Lock != "gold watch" {
		t.Fatalf("separate item lock missing: %+v", villain.Items)
	}
}
