package flow

import (
	"SnakeMedia/internal/providers/flow/session"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
)

// videoModelsPayload follows the captured HTrJv payload, trimmed to video fields.
const videoModelsPayload = `[[null,null,` +
	`[[1,[null,null,"beluga_display","veo_3_1_lite"]],[3,[null,null,"beluga_display","abra"]]],` +
	`[],` +
	`[["Omni 1.1 Flash",[` +
	`["abra_t2v_4s",null,null,null,[[1,[[null,7]]],[3,[[null,7]]]],null,null,[[[[1]]]],null,null,true,null,[[2,1]],null,120,null,4,true,null,null,null,[],null,[[1]]],` +
	`["abra_t2v_8s",null,null,null,[[1,[[null,12]]],[3,[[null,12]]]],null,null,[[[[1]]]],null,null,true,null,[[2,1]],null,120,null,8,true,null,null,null,[],null,[[1]]],` +
	`["abra_r2v_8s",null,null,null,[[1,[[null,12]]],[3,[[null,12]]]],null,null,[[[[1,6]],[[1,6,18,19]],[[1,6,18]]]],null,7,true,null,[[2,1]],null,120,null,8,true,null,null,null,[5,3,7],null,[[1]]],` +
	`["abra_t2v_8s_360p",null,null,null,[[1,[[null,6]]],[3,[[null,6]]]],null,null,[[[[1]]]],null,null,true,null,[[2,1]],null,120,null,8,true,null,null,null,[],null,[[4]]],` +
	`["abra_i2v_8s",null,null,null,[[1,[[null,12]]],[3,[[null,12]]]],null,null,[[[[1,5]]]],null,null,true,null,[[2,1]],null,120,null,8,true,null,null,null,[],null,[[1]]]` +
	`],true,"abra"],` +
	`["Veo 3.1 - Fast",[` +
	`["veo_3_1_t2v_fast",null,null,null,[[1,[[null,20]]],[3,[null,[]]]],null,null,[[[[1]]]],null,null,true,null,[[2]],null,120,null,8,true,null,null,null,[],null,[[1]]],` +
	`["veo_3_1_t2v_fast_ultra",null,null,null,[[1,[null,[]]],[3,[[null,10]]]],null,null,[[[[1]]]],null,null,true,null,[[2]],null,120,null,8,true,null,null,null,[],null,[[1]]],` +
	`["veo_3_1_t2v_fast_portrait",null,null,null,[[1,[[null,20]]],[3,[null,[]]]],null,null,[[[[1]]]],null,null,true,null,[[1]],null,120,null,8,true,null,null,null,[],null,[[1]]],` +
	`["veo_3_1_r2v_fast_landscape",null,null,null,[[1,[[null,20]]],[3,[null,[]]]],null,null,[[[[1,6]],[[1,6,18,19]],[[1,6,18]]]],null,3,true,null,[[2]],null,120,null,8,true,null,null,null,[1,3,3],null,[[1]]],` +
	`["veo_3_1_t2v_fast_4s",null,null,null,[[1,[null,[]]],[3,[[null,10]]]],null,null,[[[[1]]]],null,null,true,null,[[2,1]],null,120,null,4,true,null,null,null,[],null,[[1]]]` +
	`],true,"veo_3_1_fast"],` +
	`["Veo 3.1 - Upsampler 1080P",[["veo_3_1_upsampler_1080p",null,null,null,[[1,[[null,5]]]],null,null,[[[[13]]]]]],true,"veo_3_1_upsampler"]` +
	`],[],null,[]]]`

func TestVideoModelsFollowTierAndCapabilities(t *testing.T) {
	families, defaultID, err := parseVideoFamilies(videoModelsPayload, 1)
	if err != nil {
		t.Fatal(err)
	}
	if defaultID != "veo_3_1_lite" {
		t.Fatalf("tier 1 default = %q", defaultID)
	}
	list := videoModels(families, "abra", 1)
	if len(list) != 2 {
		t.Fatalf("expected Omni and Veo Fast only, got %+v", list)
	}
	omni, fast := list[0], list[1]
	if omni.ID != "abra" || !omni.Default || jsonString(omni.Durations) != "[4,8]" {
		t.Fatalf("unexpected Omni model: %+v", omni)
	}
	if fast.ID != "veo_3_1_fast" || jsonString(fast.Durations) != "[8]" || jsonString(fast.AspectRatios) != `["16:9","9:16"]` {
		t.Fatalf("tier 1 Veo Fast must offer 8s in both ratios: %+v", fast)
	}
	if jsonString(omni.Resolutions) != `["720p","360p"]` || jsonString(fast.Resolutions) != `["720p"]` {
		t.Fatalf("qualities not read from the usages: omni %v fast %v", omni.Resolutions, fast.Resolutions)
	}

	byID := map[string]videoFamily{}
	for _, f := range families {
		byID[f.id] = f
	}
	for _, tc := range []struct {
		family                string
		tier, aspect, ds, res int
		want                  string
	}{
		{"veo_3_1_fast", 1, 2, 8, 1, "veo_3_1_t2v_fast"},
		{"veo_3_1_fast", 1, 1, 8, 1, "veo_3_1_t2v_fast_portrait"},
		{"veo_3_1_fast", 3, 2, 8, 1, "veo_3_1_t2v_fast_ultra"},
		{"veo_3_1_fast", 3, 1, 4, 1, "veo_3_1_t2v_fast_4s"},
		{"abra", 1, 1, 8, 1, "abra_t2v_8s"},
		{"abra", 1, 2, 8, 4, "abra_t2v_8s_360p"},
	} {
		got, err := selectVideoVariant(byID[tc.family], tc.tier, tc.aspect, tc.ds, tc.res, false)
		if err != nil || got.key != tc.want {
			t.Errorf("%s tier %d aspect %d %ds: got %q %v, want %q", tc.family, tc.tier, tc.aspect, tc.ds, got.key, err, tc.want)
		}
	}
	if _, err := selectVideoVariant(byID["veo_3_1_fast"], 1, 2, 4, 1, false); err == nil {
		t.Fatal("a 4s variant unavailable on tier 1 was selected")
	}
	if _, err := selectVideoVariant(byID["veo_3_1_fast"], 1, 2, 8, 4, false); err == nil {
		t.Fatal("a 360p variant Veo Fast does not have was selected")
	}

	// Reference images: the r2v variant with its own limit, never a text variant.
	if omni.MaxReferenceImages != 7 || fast.MaxReferenceImages != 3 {
		t.Fatalf("reference limits not read from r2v usages: omni %d fast %d", omni.MaxReferenceImages, fast.MaxReferenceImages)
	}
	for _, tc := range []struct {
		family       string
		aspect, refs int
		want         string
	}{
		{"abra", 1, 7, "abra_r2v_8s"},
		{"veo_3_1_fast", 2, 3, "veo_3_1_r2v_fast_landscape"},
	} {
		got, err := selectVideoVariant(byID[tc.family], 1, tc.aspect, 8, 1, true)
		if err != nil || got.key != tc.want || got.maxRefs != tc.refs {
			t.Errorf("%s with references: got %+v %v, want %s (%d refs)", tc.family, got, err, tc.want, tc.refs)
		}
	}
	if _, err := selectVideoVariant(byID["veo_3_1_fast"], 1, 1, 8, 1, true); err == nil {
		t.Fatal("a portrait reference variant the fixture lacks was selected")
	}
	if _, err := selectVideoVariant(byID["abra"], 1, 2, 4, 1, true); err == nil {
		t.Fatal("references fell back to a text-only variant")
	}
}

func jsonString(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestBuildVideoTextRequest(t *testing.T) {
	data, err := BuildVideoTextRequest("a red fox", "proj", "abra_t2v_8s", 2, 1, 2, "tok")
	if err != nil {
		t.Fatal(err)
	}
	var root []any
	if err := json.Unmarshal(data, &root); err != nil || len(root) != 3 {
		t.Fatalf("unexpected request: %s", data)
	}
	items := root[0].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	item := items[0].([]any)
	if jsonString(item[0]) != `[null,null,[[["a red fox"]]]]` || item[1] != "abra_t2v_8s" || item[2] != float64(2) {
		t.Fatalf("unexpected item: %s", jsonString(item))
	}
	if tail := item[4].([]any); len(tail) != 6 || tail[4] == nil || tail[5] == nil || tail[4] == tail[5] {
		t.Fatalf("item must end with two client ids (SolwStudio capture): %s", jsonString(tail))
	}
	if ctx := root[1].([]any); ctx[1] != float64(FlowSurfaceID) || ctx[5] != "proj" || jsonString(ctx[10]) != `["tok",1]` {
		t.Fatalf("unexpected context: %s", jsonString(ctx))
	}
	if trailer := root[2].([]any); len(trailer) != 2 || trailer[1] != float64(2) {
		t.Fatalf("request must end with [batchID, 2]: %s", jsonString(trailer))
	}
	if len(item) != 5 {
		t.Fatalf("720p items carry no resolution field: %s", jsonString(item))
	}
	data, err = BuildVideoTextRequest("a red fox", "proj", "abra_t2v_8s_360p", 2, 4, 1, "tok")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(data, &root)
	if item := root[0].([]any)[0].([]any); len(item) != 8 || jsonString(item[7]) != "[4]" {
		t.Fatalf("360p item must carry field 8 = [4]: %s", jsonString(item))
	}
	for _, bad := range [][]any{{"", "proj", "k", 2, 1}, {"p", "", "k", 2, 1}, {"p", "proj", "k", 3, 1}, {"p", "proj", "k", 2, 5}} {
		if _, err := BuildVideoTextRequest(bad[0].(string), bad[1].(string), bad[2].(string), bad[3].(int), 1, bad[4].(int), "tok"); err == nil {
			t.Errorf("accepted invalid request %v", bad)
		}
	}
}

func TestParseVideoMediaStatusAndURL(t *testing.T) {
	payload := `[null,null,[` +
		`["media-1","proj",null,null,null,[null,null,null,null,null,null,null,null,[3]]],` +
		`["media-2","proj",null,null,null,[null,null,null,null,null,null,null,null,[7,[3,"blocked"]]]],` +
		`["media-3","proj"]]]`
	media, err := parseVideoMedia(payload, 2)
	if err != nil || len(media) != 3 {
		t.Fatalf("unexpected media %+v %v", media, err)
	}
	if media[0].status != 3 {
		t.Errorf("finished media misread: %+v", media[0])
	}
	if media[1].status != 7 || !strings.Contains(media[1].errMsg, "blocked") {
		t.Errorf("failed media misread: %+v", media[1])
	}
	if media[2].status != 0 {
		t.Errorf("media without state must stay unknown: %+v", media[2])
	}

	media1 := []any{"media-1", []any{"https://flow-content.google/image/thumb?sig=a", []any{"https://flow-content.google/video/media-1?sig=b"}}}
	if got := videoURL(media1); got != "https://flow-content.google/video/media-1?sig=b" {
		t.Errorf("video URL not picked over the thumbnail: %q", got)
	}
}

func TestWaitForVideosPollsUntilSettled(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	stored, err := accounts.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	mediaCalls := 0
	http.DefaultTransport = flowRoundTrip(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		quote := func(v any) string {
			payload, _ := json.Marshal(v)
			quoted, _ := json.Marshal(string(payload))
			return string(quoted)
		}
		switch rpc := r.URL.Query().Get("rpcids"); rpc {
		case RPCGetProjectContents:
			if !strings.Contains(form.Get("f.req"), `projects/proj`) {
				t.Errorf("listing for the wrong project: %s", form.Get("f.req"))
			}
			// The listing lags: media-1 still pending, media-2 blocked by policy.
			state := func(code int) []any { return []any{nil, nil, nil, nil, nil, nil, nil, nil, []any{code}} }
			return flowReply(r, rpc, quote([]any{nil, nil, []any{
				[]any{"media-1", "proj", nil, nil, nil, state(2)},
				[]any{"media-2", "proj", nil, nil, nil, state(7)},
			}})), nil
		case RPCGetMedia:
			if strings.Contains(form.Get("f.req"), "media-2") {
				t.Error("a failed video was polled again")
			}
			mediaCalls++
			if mediaCalls == 1 {
				// Not served yet: an error envelope, which is not a poll failure.
				envelope := `)]}'` + "\n\n1\n" + `[["wrb.fr","as29s",null,null,null,[5],"generic"]]`
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(envelope)), Request: r}, nil
			}
			return flowReply(r, rpc, quote([]any{"media-1", []any{"https://flow-content.google/video/media-1?sig=b"}})), nil
		}
		t.Errorf("unexpected RPC %s", r.URL.RawQuery)
		return nil, io.EOF
	})
	service := testFlowService(accounts)
	client, err := chrome.NewFlowClient(stored.Credentials, accounts.cfgManager.Get().Accounts)
	if err != nil {
		t.Fatal(err)
	}
	final, err := service.waitForVideos(context.Background(), client, "proj", []pendingVideo{{name: "media-1"}, {name: "media-2"}}, 1, 60)
	if err != nil {
		t.Fatal(err)
	}
	if mediaCalls != 2 || len(final) != 2 {
		t.Fatalf("polling stopped early or late: mediaCalls=%d final=%+v", mediaCalls, final)
	}
	if final[0].url != "https://flow-content.google/video/media-1?sig=b" || final[0].status != videoStatusSuccess {
		t.Errorf("finished video misread: %+v", final[0])
	}
	if final[1].url != "" || final[1].status != 7 {
		t.Errorf("policy failure misread: %+v", final[1])
	}
}

func TestGenerateVideosValidatesAndSelectsVariant(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return videoModelsPayload, nil }
	var gotVariant string
	service.generateLiveVideos = func(_ context.Context, _ session.StoredAccount, _ GenerateVideoRequest, variant string) ([]GeneratedVideoItem, error) {
		gotVariant = variant
		return []GeneratedVideoItem{{ID: "media-1"}}, nil
	}
	valid := GenerateVideoRequest{Prompt: "a fox", ModelID: "veo_3_1_fast", AspectRatio: "9:16", Duration: 8, Resolution: "720p", Count: 1}
	if res := service.GenerateVideos(context.Background(), valid); !res.Success || gotVariant != "veo_3_1_t2v_fast_portrait" {
		t.Fatalf("unexpected result %+v variant %q", res, gotVariant)
	}
	for name, mutate := range map[string]func(*GenerateVideoRequest){
		"no prompt":     func(r *GenerateVideoRequest) { r.Prompt = " " },
		"no model":      func(r *GenerateVideoRequest) { r.ModelID = "" },
		"square":        func(r *GenerateVideoRequest) { r.AspectRatio = "1:1" },
		"no duration":   func(r *GenerateVideoRequest) { r.Duration = 0 },
		"too many":      func(r *GenerateVideoRequest) { r.Count = MaxVideosPerRequest + 1 },
		"unknown model": func(r *GenerateVideoRequest) { r.ModelID = "nope" },
		"tier lacks 4s": func(r *GenerateVideoRequest) { r.Duration = 4 },
		"no quality":    func(r *GenerateVideoRequest) { r.Resolution = "" },
		"no 360p":       func(r *GenerateVideoRequest) { r.Resolution = "360p" },
	} {
		req := valid
		mutate(&req)
		if res := service.GenerateVideos(context.Background(), req); res.Success || res.Code != models.CodeInvalidArgument {
			t.Errorf("%s: expected CodeInvalidArgument, got %+v", name, res)
		}
	}
}

func TestBuildVideoReferenceRequest(t *testing.T) {
	data, err := BuildVideoReferenceRequest("a fox", "proj", "abra_r2v_8s", 2, 1, 1, []string{"m1", "m2"}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	var root []any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	item := root[0].([]any)[0].([]any)
	if len(item) != 6 || jsonString(item[1]) != `[[null,"m1"],[null,"m2"]]` || item[2] != "abra_r2v_8s" || item[3] != float64(2) {
		t.Fatalf("item differs from the SolwStudio capture: %s", jsonString(item))
	}
	if trailer := root[2].([]any); len(trailer) != 2 || trailer[1] != float64(2) {
		t.Fatalf("request must end with [batchID, 2]: %s", jsonString(trailer))
	}
	data, err = BuildVideoReferenceRequest("a fox", "proj", "abra_r2v_8s_360p", 2, 4, 1, []string{"m1"}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(data, &root)
	if item := root[0].([]any)[0].([]any); len(item) != 12 || jsonString(item[11]) != "[4]" {
		t.Fatalf("360p reference item must carry field 12 = [4]: %s", jsonString(item))
	}
	if _, err := BuildVideoReferenceRequest("a fox", "proj", "k", 2, 1, 1, nil, "tok"); err == nil {
		t.Fatal("a reference request without references was built")
	}
}

func TestOperationRepliesResolveThroughListing(t *testing.T) {
	ops := parseVideoOperations(`[null,50,[["op-9","proj","scene-1","PENDING"]]]`)
	if len(ops) != 1 || ops[0].operation != "op-9" || ops[0].name != "" {
		t.Fatalf("operation not read: %+v", ops)
	}
	// Sample from SolwStudio's test_flow_rpc.py: the operation, then the media slot.
	listing := `xx "OP-9" ,["t",[1,2],null,null,"11111111-2222-3333-4444-555555555555"`
	if got := mediaForOperation(listing, "op-9"); got != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("media id not found next to the operation: %q", got)
	}
	if got := mediaForOperation(listing, "op-404"); got != "" {
		t.Fatalf("unknown operation matched %q", got)
	}
}

func TestGenerateVideosEnforcesReferenceLimit(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return videoModelsPayload, nil }
	var gotVariant string
	service.generateLiveVideos = func(_ context.Context, _ session.StoredAccount, _ GenerateVideoRequest, variant string) ([]GeneratedVideoItem, error) {
		gotVariant = variant
		return []GeneratedVideoItem{{ID: "media-1"}}, nil
	}
	ref := ReferenceImage{Base64: "AAAA", MimeType: "image/png", FileName: "a.png"}
	req := GenerateVideoRequest{Prompt: "a fox", ModelID: "veo_3_1_fast", AspectRatio: "16:9", Duration: 8, Resolution: "720p", Count: 1,
		References: []ReferenceImage{ref, ref, ref}}
	if res := service.GenerateVideos(context.Background(), req); !res.Success || gotVariant != "veo_3_1_r2v_fast_landscape" {
		t.Fatalf("references within the limit rejected: %+v variant %q", res, gotVariant)
	}
	req.References = append(req.References, ref)
	if res := service.GenerateVideos(context.Background(), req); res.Success || res.Code != models.CodeInvalidArgument || !strings.Contains(res.Error, "tối đa 3") {
		t.Fatalf("a fourth Veo reference was accepted: %+v", res)
	}
	req.References = []ReferenceImage{{Base64: "AAAA", MimeType: "image/gif", FileName: "a.gif"}}
	if res := service.GenerateVideos(context.Background(), req); res.Success || res.Code != models.CodeInvalidArgument {
		t.Fatalf("an unsupported reference format was accepted: %+v", res)
	}
}
