package flow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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
	if start, end, _ := editSourceFrames(8, 0, 0, u.maxInputSeconds); start != 0 || end != 240 {
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

func TestEditVideoFailureDetailNamesSourceAndPrompt(t *testing.T) {
	detail := editVideoFailureDetail(`[3,"PUBLIC_ERROR_DANGER_FILTER"]`)
	if !strings.Contains(detail, "video nguồn") || !strings.Contains(detail, "yêu cầu chỉnh sửa") || strings.Contains(detail, publicErrorDangerFilter) {
		t.Fatalf("edit safety rejection lacks source-aware guidance: %q", detail)
	}

	job := editJob{Complete: true, Results: []GeneratedVideoItem{{Error: `[3,"PUBLIC_ERROR_DANGER_FILTER"]`}}}
	items := job.items()
	if len(items) != 1 || !strings.Contains(items[0].Error, "video nguồn") {
		t.Fatalf("persisted edit error was not normalized: %+v", items)
	}
}

func TestUploadedSourceKeepsOwnershipAcrossRotationAndRestart(t *testing.T) {
	accounts := testAccountService(t)
	owner := seedFlowAccount(t, accounts, "upload@example.invalid", true)
	seedFlowAccount(t, accounts, "other@example.invalid", true)
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

	service.uploadLiveVideo = func(_ context.Context, _ session.StoredAccount, source localVideo) (sourceRecord, error) {
		return sourceRecord{MediaID: "uploaded-mid-123", ProjectID: "22d33fe3-5afe-4106-8913-dc66336677f8", WorkflowID: "wf-123", Video: UploadedSourceVideo{DurationSeconds: 8, DisplayName: source.fileName, Width: 1920, Height: 1080}}, nil
	}

	upRes := service.UploadSourceVideo(context.Background(), imported.Data.LocalURL)
	if !upRes.Success || upRes.Data.SourceID == "" || upRes.Data.Width != 1920 {
		t.Fatalf("unexpected upload result: %+v", upRes)
	}

	// Other operations advance rotation, and the provider is recreated.
	_, _ = service.NextAccount()
	service = testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return editModelsPayload, nil }
	var passedReq EditVideoRequest
	service.editLiveVideos = func(_ context.Context, selected session.StoredAccount, req EditVideoRequest, _ localVideo, _ videoFamily) ([]GeneratedVideoItem, error) {
		if selected.ID != owner.ID {
			t.Errorf("source crossed account boundary")
		}
		passedReq = req
		return []GeneratedVideoItem{{ID: "gen-1"}}, nil
	}

	editReq := EditVideoRequest{
		Prompt:      "cinematic pan",
		ModelID:     "abra",
		SourceURL:   imported.Data.LocalURL,
		SourceID:    upRes.Data.SourceID,
		TrimStartMS: 500,
		TrimEndMS:   4500,
	}
	editRes := service.EditVideos(context.Background(), editReq)
	if !editRes.Success || passedReq.source == nil || passedReq.source.MediaID != "uploaded-mid-123" {
		t.Fatalf("expected edit to succeed with passed MediaID, got: %+v", editRes)
	}
}

func TestEditRangeUsesActualSourceTimeline(t *testing.T) {
	for _, tc := range []struct {
		duration                          float64
		startMS, endMS, limit, start, end int
		invalid                           bool
	}{
		{6, 0, 0, 10, 0, 180, false},
		{12, 5000, 9000, 10, 150, 270, false},
		{2.5, 0, 2500, 10, 0, 75, false},
		{6, 0, 7000, 10, 0, 0, true},
		{20, 0, 11000, 10, 0, 0, true},
		{0, 0, 0, 10, 0, 0, true},
	} {
		first, last, err := editSourceFrames(tc.duration, tc.startMS, tc.endMS, tc.limit)
		if tc.invalid {
			if err == nil {
				t.Error("invalid range accepted")
			}
			continue
		}
		if err != nil || first != tc.start || last != tc.end {
			t.Errorf("range: got %d-%d %v expected %d-%d", first, last, err, tc.start, tc.end)
		}
	}
}

func TestVideoOutputNeverUsesReferenceURL(t *testing.T) {
	sourceURL := "https://flow-content.google/video/input"
	if got := videoOutputURL([]any{"pending", nil, nil, nil, nil, []any{sourceURL}}); got != "" {
		t.Fatal("input treated as finished output")
	}
	outputURL := "https://flow-content.google/video/output"
	record := []any{"output", nil, nil, nil, nil, []any{sourceURL}, nil, []any{nil, nil, nil, nil, []any{nil, nil, outputURL}}}
	if got := videoOutputURL(record); got != outputURL {
		t.Fatalf("wrong output URL %q", got)
	}
}

func TestMaterializedOutputURLMatchesLiveGetMedia(t *testing.T) {
	expected := "https://flow-content.google/video/result"
	descriptor := make([]any, 9)
	descriptor[8] = expected
	record := []any{"result", nil, nil, nil, nil, nil, nil, []any{descriptor}}
	if got := videoOutputURL(record); got != expected {
		t.Fatalf("materialized serving URL was lost: %q", got)
	}
	descriptor[8] = "https://lh3.googleusercontent.com/thumbnail"
	if got := videoOutputURL(record); got != "" {
		t.Fatal("thumbnail treated as output")
	}
}

func TestEditCandidatesReserveTheMostAnEditCosts(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "edit@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return editModelsPayload, nil }
	valid, err := service.GetValidConnectedAccounts()
	if err != nil || len(valid) != 1 {
		t.Fatalf("seeded account not usable: %v", err)
	}
	acc := valid[0]

	families, defaultID, err := parseVideoFamilies(editModelsPayload, accountTier(acc))
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, m := range editModels(families, defaultID, accountTier(acc)) {
		want = max(want, m.MaxCredits)
	}
	if want == 0 {
		t.Fatal("the fixture must price its edit usages")
	}
	candidates := service.editCandidates(context.Background(), []session.StoredAccount{acc})
	if len(candidates) != 1 || candidates[0].account.ID != acc.ID || candidates[0].cost != want {
		t.Fatalf("want one candidate costing %d, got %+v", want, candidates)
	}
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return "", errors.New("offline") }
	service = testFlowService(accounts)
	if got := service.editCandidates(context.Background(), []session.StoredAccount{acc}); len(got) != 0 {
		t.Fatalf("an account without a catalogue must not be offered: %+v", got)
	}
}
