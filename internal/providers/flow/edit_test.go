package flow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/session"
)

// editModelsPayload adds the captured Omni edit usages (abra_edit, abra_edit_360p)
// to a trimmed HTrJv payload.
const editModelsPayload = `[[null,null,` +
	`[[1,[null,null,"beluga_display","abra"]]],` +
	`[],` +
	`[["Omni 1.1 Flash",[` +
	`["abra_t2v_8s",null,null,null,[[1,[[null,12]]]],null,null,[[[[1]]]],null,null,true,null,[[2,1]],null,120,null,8,true,null,null,null,[],null,[[1]]],` +
	`["abra_r2v_8s",null,null,null,[[1,[[null,12]]]],null,null,[[[[1,6]],[[1,6,18,19]],[[1,6,18]]]],null,7,true,null,[[2,1]],null,120,null,8,true,null,null,null,[5,3,7],null,[[1]]],` +
	`["abra_edit",null,null,null,[[1,[[null,20]]],[2,[[null,20]]],[3,[[null,20]]]],null,null,[[[[1,6,18,19,20]]]],null,5,true,null,[[2,1]],null,160,null,null,true,null,null,null,[3,3,5,10],true,[[1]]],` +
	`["abra_edit_360p",null,null,null,[[1,[[null,10]]],[2,[[null,10]]],[3,[[null,10]]]],null,null,[[[[1,6,18,19,20]]]],null,5,true,null,[[2,1]],null,160,null,null,true,null,null,null,[3,3,5,10],true,[[4]]]` +
	`],true,"abra"],` +
	`["Veo 3.1 - Fast",[` +
	`["veo_3_1_t2v_fast",null,null,null,[[1,[[null,20]]]],null,null,[[[[1]]]],null,null,true,null,[[2]],null,120,null,8,true,null,null,null,[],null,[[1]]]` +
	`],true,"veo_3_1_fast"]` +
	`],[],null,[]]]`

func TestEditModelsComeFromVideoInputUsages(t *testing.T) {
	families, defaultID, err := parseVideoFamilies(editModelsPayload, 1)
	if err != nil {
		t.Fatal(err)
	}
	list := editModels(families, defaultID, 1)
	if len(list) != 1 {
		t.Fatalf("only Omni edits video: %+v", list)
	}
	if omni := list[0]; omni.ID != "abra" || !omni.Default || omni.Duration != 8 || omni.MaxInputSeconds != 10 {
		t.Fatalf("unexpected edit model %+v", omni)
	}
	// Edit usages must not leak into text-to-video or reference-to-video.
	text := videoModels(families, defaultID, 1)
	if len(text) == 0 || jsonString(text[0].Durations) != "[8]" || jsonString(text[0].Resolutions) != `["720p"]` {
		t.Fatalf("text models changed: %+v", text)
	}
	if u, err := selectVideoVariant(families[0], 1, 2, 8, 1, true); err != nil || u.key != "abra_r2v_8s" {
		t.Fatalf("references picked %q %v", u.key, err)
	}
	// Uploads are 720p in Flow's editor.
	u, resolution, err := selectEditVariant(families[0], 1, 2)
	if err != nil || u.key != "abra_edit" || resolution != 1 {
		t.Fatalf("picked %q at %d: %v", u.key, resolution, err)
	}
	if start, end := sourceFrames(u); start != 0 || end != 240 {
		t.Fatalf("source frames %d-%d, Flow sends 0-240", start, end)
	}
	// A tier without the 720p edit falls back to 360p.
	only360 := families[0]
	only360.usages = slices.DeleteFunc(slices.Clone(only360.usages), func(u videoUsage) bool { return u.key == "abra_edit" })
	if u, resolution, err := selectEditVariant(only360, 1, 1); err != nil || u.key != "abra_edit_360p" || resolution != 4 {
		t.Fatalf("fallback picked %q at %d: %v", u.key, resolution, err)
	}
	if _, _, err := selectEditVariant(families[1], 1, 2); err == nil {
		t.Fatal("Veo Fast has no edit usage")
	}
}

func TestEditAspectFollowsSourceSize(t *testing.T) {
	for _, tc := range []struct {
		w, h   int
		name   string
		aspect int
	}{{1280, 720, "16:9", 2}, {1024, 768, "16:9", 2}, {720, 1280, "9:16", 1}, {768, 1024, "9:16", 1}, {0, 0, "16:9", 2}} {
		name, aspect, err := editAspect(tc.w, tc.h)
		if err != nil || name != tc.name || aspect != tc.aspect {
			t.Errorf("%dx%d: %q %d %v", tc.w, tc.h, name, aspect, err)
		}
	}
	if _, _, err := editAspect(1080, 1080); err == nil {
		t.Error("a square video has no edit aspect")
	}
}

var clientIDPattern = regexp.MustCompile(`[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}`)

func TestBuildEditVideoRequestMatchesCapture(t *testing.T) {
	inner, err := BuildEditVideoRequest("A cinematic camera pan around the subject, golden hour lighting", "22d33fe3-5afe-4106-8913-dc66336677f8", "abra_edit_360p", "8eb41f86-bae8-4452-b5bb-39406e6343ee", 0, 240, 2, 4, "TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	// The jIps6 capture with its client IDs replaced.
	want := `[[[[null,"8eb41f86-bae8-4452-b5bb-39406e6343ee",0,240],[null,null,[[["A cinematic camera pan around the subject, golden hour lighting"]]]],"abra_edit_360p",2,[null,null,null,null,"ID","ID"],null,null,null,null,null,null,null,[4]]],[null,22,null,null,null,"22d33fe3-5afe-4106-8913-dc66336677f8",null,null,null,null,["TOKEN",1]],["ID",2]]`
	if got := clientIDPattern.ReplaceAllString(string(inner), "ID"); got != want {
		t.Fatalf("payload differs from capture:\n got %s\nwant %s", got, want)
	}
	inner, _ = BuildEditVideoRequest("p", "proj", "abra_edit", "mid", 0, 240, 1, 1, "T")
	var decoded []any
	if err := json.Unmarshal(inner, &decoded); err != nil {
		t.Fatal(err)
	}
	items := decoded[0].([]any)
	if len(items) != 1 || len(items[0].([]any)) != 5 {
		t.Fatalf("720p must omit field 13 and send one item: %s", inner)
	}
	if _, err := BuildEditVideoRequest("p", "proj", "abra_edit", "mid", 10, 5, 2, 1, "T"); err == nil {
		t.Fatal("an empty source range must be refused")
	}
}

func TestEditReplyIsFollowedLikeTextToVideo(t *testing.T) {
	// Inner payload of the captured jIps6 reply.
	payload := `[null,48,[["289eb7d5-6caa-4a48-909b-ad6e7cfbdecf",null,null,["Camera panning around subject",[1792757794,133516000],null,null,"5ab01e11-ecec-4b27-a22f-9480de4c4350","3E2273CB-CCC6-40A8-BC7C-01B5BBCAD35B",[1792757797,730371000]],"22d33fe3-5afe-4106-8913-dc66336677f8"]],[["5ab01e11-ecec-4b27-a22f-9480de4c4350","22d33fe3-5afe-4106-8913-dc66336677f8","289eb7d5-6caa-4a48-909b-ad6e7cfbdecf",null,null,[[1792757794,133516000],"A cinematic camera pan around the subject, golden hour lighting",null,null,null,null,[null,[["abra_edit_360p",4,[10],null,2,4],null,[[null,null,null,"8eb41f86-bae8-4452-b5bb-39406e6343ee"]]],[[null,null,[[["A cinematic camera pan around the subject, golden hour lighting"]]]]],null,1],null,[6],1],null,[[null,0,null,null,null,null,null,"A cinematic camera pan around the subject, golden hour lighting",null,null,null,null,"abra_edit_360p","",null,false,2],[null,null,[10]],["5ab01e11-ecec-4b27-a22f-9480de4c4350"]]]]]`
	started, err := parseVideoMedia(payload, 3)
	if err != nil || len(started) != 1 || started[0].name != "5ab01e11-ecec-4b27-a22f-9480de4c4350" || started[0].status != 6 {
		t.Fatalf("unexpected %+v %v", started, err)
	}
}

func TestEditVideosValidatesAndSelectsVariant(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return editModelsPayload, nil }
	src := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(src, []byte("MP4"), 0o600); err != nil {
		t.Fatal(err)
	}
	imported := service.media.ImportVideo(func() (string, error) { return src, nil })
	if !imported.Success {
		t.Fatal(imported.Error)
	}
	var gotFamily string
	var gotSource localVideo
	service.editLiveVideos = func(_ context.Context, _ session.StoredAccount, _ EditVideoRequest, source localVideo, family videoFamily) ([]GeneratedVideoItem, error) {
		gotFamily, gotSource = family.id, source
		return []GeneratedVideoItem{{ID: "media-1"}}, nil
	}
	valid := EditVideoRequest{Prompt: "golden hour", ModelID: "abra", SourceURL: imported.Data.LocalURL, TrimStartMS: 1000, TrimEndMS: 11000}
	res := service.EditVideos(context.Background(), valid)
	if !res.Success || gotFamily != "abra" || gotSource.contentType != "video/mp4" || res.Data[0].ModelName != "Omni 1.1 Flash" {
		t.Fatalf("unexpected result %+v family %q source %+v", res, gotFamily, gotSource)
	}
	for name, mutate := range map[string]func(*EditVideoRequest){
		"no prompt":     func(r *EditVideoRequest) { r.Prompt = " " },
		"no source":     func(r *EditVideoRequest) { r.SourceURL = "" },
		"outside store": func(r *EditVideoRequest) { r.SourceURL = "/flow-media/../accounts.json" },
		"missing file":  func(r *EditVideoRequest) { r.SourceURL = "/flow-media/gone.mp4" },
		"no edit model": func(r *EditVideoRequest) { r.ModelID = "veo_3_1_fast" },
		"reversed trim": func(r *EditVideoRequest) { r.TrimStartMS, r.TrimEndMS = 5000, 1000 },
		"trim too long": func(r *EditVideoRequest) { r.TrimStartMS, r.TrimEndMS = 0, 10_001 },
		"unknown model": func(r *EditVideoRequest) { r.ModelID = "nope" },
	} {
		req := valid
		mutate(&req)
		if res := service.EditVideos(context.Background(), req); res.Success || res.Code != models.CodeInvalidArgument {
			t.Errorf("%s: expected CodeInvalidArgument, got %+v", name, res)
		}
	}
}

func TestUploadSourceVideoAndEditWithMediaID(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "upload@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return editModelsPayload, nil }

	src := filepath.Join(t.TempDir(), "upload_clip.mp4")
	if err := os.WriteFile(src, []byte("MP4"), 0o600); err != nil {
		t.Fatal(err)
	}
	imported := service.media.ImportVideo(func() (string, error) { return src, nil })
	if !imported.Success {
		t.Fatal(imported.Error)
	}

	service.uploadLiveVideo = func(_ context.Context, _ session.StoredAccount, source localVideo) (UploadedSourceVideo, error) {
		return UploadedSourceVideo{
			MediaID:     "uploaded-mid-123",
			WorkflowID:  "wf-123",
			DisplayName: source.fileName,
			Width:       1920,
			Height:      1080,
		}, nil
	}

	upRes := service.UploadSourceVideo(context.Background(), imported.Data.LocalURL)
	if !upRes.Success || upRes.Data.MediaID != "uploaded-mid-123" || upRes.Data.Width != 1920 {
		t.Fatalf("unexpected upload result: %+v", upRes)
	}

	var passedReq EditVideoRequest
	service.editLiveVideos = func(_ context.Context, _ session.StoredAccount, req EditVideoRequest, _ localVideo, _ videoFamily) ([]GeneratedVideoItem, error) {
		passedReq = req
		return []GeneratedVideoItem{{ID: "gen-1"}}, nil
	}

	editReq := EditVideoRequest{
		Prompt:      "cinematic pan",
		ModelID:     "abra",
		SourceURL:   imported.Data.LocalURL,
		MediaID:     upRes.Data.MediaID,
		Width:       upRes.Data.Width,
		Height:      upRes.Data.Height,
		TrimStartMS: 500,
		TrimEndMS:   4500,
	}
	editRes := service.EditVideos(context.Background(), editReq)
	if !editRes.Success || passedReq.MediaID != "uploaded-mid-123" {
		t.Fatalf("expected edit to succeed with passed MediaID, got: %+v", editRes)
	}
}
