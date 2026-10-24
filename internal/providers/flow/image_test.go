package flow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/providers/flow/chrome"
)

func TestValidateAccountCookies(t *testing.T) {
	t.Run("empty cookies fail", func(t *testing.T) {
		err := ValidateAccountCookies(nil)
		if err == nil {
			t.Fatal("expected error for nil cookies")
		}
	})

	t.Run("missing core cookies fail", func(t *testing.T) {
		cookies := []chrome.CookieItem{
			{Name: "_ga", Value: "123"},
			{Name: "NID", Value: "456"},
		}
		err := ValidateAccountCookies(cookies)
		if err == nil {
			t.Fatal("expected error for missing core cookies")
		}
	})

	t.Run("expired core cookie fails", func(t *testing.T) {
		past := float64(time.Now().Add(-1 * time.Hour).Unix())
		cookies := []chrome.CookieItem{
			{Name: "SID", Value: "val", Expires: past},
			{Name: "HSID", Value: "val"},
			{Name: "SSID", Value: "val"},
		}
		err := ValidateAccountCookies(cookies)
		if err == nil {
			t.Fatal("expected error for expired core cookie")
		}
	})

	t.Run("valid cookies succeed", func(t *testing.T) {
		future := float64(time.Now().Add(24 * time.Hour).Unix())
		cookies := []chrome.CookieItem{
			{Name: "__Secure-1PSID", Value: "val", Expires: future},
			{Name: "HSID", Value: "val"},
			{Name: "__Secure-3PAPISID", Value: "val", Expires: future},
		}
		err := ValidateAccountCookies(cookies)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestBuildImageBatchRequest(t *testing.T) {
	data, err := BuildImageBatchRequest("cyberpunk city", "proj-test", "16:9", "GEM_PIX_2", 2, "test_token")
	if err != nil {
		t.Fatalf("unexpected error building payload: %v", err)
	}

	var outer []any
	if err := json.Unmarshal(data, &outer); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(outer) != 5 {
		t.Fatalf("expected outer array length 5, got %d", len(outer))
	}
	if outer[0] != nil {
		t.Fatalf("expected outer[0] to be nil, got %v", outer[0])
	}
	if outer[2] != float64(1) {
		t.Fatalf("expected outer[2] to be 1, got %v", outer[2])
	}

	items, ok := outer[1].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 generation items, got %v", outer[1])
	}

	item0, ok := items[0].([]any)
	if !ok || len(item0) != 14 {
		t.Fatalf("expected item0 to have 14 elements, got %d", len(item0))
	}

	// Index 4: aspectRatio 16:9 should be 3
	if item0[4] != float64(3) {
		t.Fatalf("expected aspect ratio 3 for 16:9, got %v", item0[4])
	}

	// Index 5: modelID
	if item0[5] != "GEM_PIX_2" {
		t.Fatalf("expected model GEM_PIX_2, got %v", item0[5])
	}

	// Index 7: context
	ctxObj, ok := item0[7].([]any)
	if !ok || len(ctxObj) != 11 {
		t.Fatalf("expected context to have 11 elements, got %v", item0[7])
	}
	if ctxObj[1] != float64(22) {
		t.Fatalf("expected SURFACE_ID 22, got %v", ctxObj[1])
	}
	if ctxObj[5] != "proj-test" {
		t.Fatalf("expected project ID proj-test, got %v", ctxObj[5])
	}

	// Index 8: prompt wrapped in 3-level array [[[prompt]]]
	promptL1, ok := item0[8].([]any)
	if !ok || len(promptL1) != 1 {
		t.Fatalf("invalid prompt wrapping level 1: %v", item0[8])
	}
	promptL2, ok := promptL1[0].([]any)
	if !ok || len(promptL2) != 1 {
		t.Fatalf("invalid prompt wrapping level 2: %v", promptL1)
	}
	promptL3, ok := promptL2[0].([]any)
	if !ok || len(promptL3) != 1 || promptL3[0] != "cyberpunk city" {
		t.Fatalf("expected prompt 'cyberpunk city', got %v", promptL2)
	}
}

func TestParseGeneratedImages(t *testing.T) {
	sampleResp := []byte(`)]}'
123
[["wrb.fr","ogiZ0b","[[\"https://flow-content.google/image/img-123?sig=abc&e=1\",\"https://flow-content.google/image/img-123?sig=abc_thumb\"],\"[\\\"https://flow-content.google/image/img-456?sig\\\\u003dxyz\\\"]\",\"https://example.com/image/not-ours\"]",null,null,null,"generic"]]`)
	payload, err := chrome.ParseRPCPayload(string(sampleResp), "ogiZ0b")
	if err != nil {
		t.Fatal(err)
	}

	items := ParseGeneratedImages(payload, "cat", "BELUGA", "1:1")
	if len(items) != 2 {
		t.Fatalf("expected 2 unique images, got %+v", items)
	}
	if items[0].ID != "img-123" || items[0].URL != "https://flow-content.google/image/img-123?sig=abc&e=1" {
		t.Errorf("escaped query characters lost: %+v", items[0])
	}
	if items[1].ID != "img-456" || items[1].URL != "https://flow-content.google/image/img-456?sig=xyz" {
		t.Errorf("URL inside nested JSON not decoded: %+v", items[1])
	}
}

func TestBuildImageBatchRequestWithReferences(t *testing.T) {
	data, err := BuildImageBatchRequest("styled girl", "proj-1", "1:1", "GEM_PIX_2", 1, "token123", []string{"media-ref-abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var outer []any
	if err := json.Unmarshal(data, &outer); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	items := outer[1].([]any)
	item0 := items[0].([]any)
	refs, ok := item0[2].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("expected 1 reference image, got %v", item0[2])
	}
	ref0, ok := refs[0].([]any)
	if !ok || len(ref0) != 5 {
		t.Fatalf("expected ref0 length 5, got %v", refs[0])
	}
	if ref0[0] != "media-ref-abc" {
		t.Errorf("expected media-ref-abc, got %v", ref0[0])
	}
	if ref0[4] != float64(1) {
		t.Errorf("expected refType 1 (IMAGE), got %v", ref0[4])
	}
}

func TestBuildImageUploadRequest(t *testing.T) {
	data, err := BuildImageUploadRequest("base64data", "image/png", "sample.png", "proj-99", "token-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var outer []any
	if err := json.Unmarshal(data, &outer); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(outer) != 12 {
		t.Fatalf("expected 12 fields in upload request, got %d", len(outer))
	}
	if outer[1] != "base64data" {
		t.Errorf("expected base64data, got %v", outer[1])
	}
	if outer[2] != "image/png" {
		t.Errorf("expected image/png, got %v", outer[2])
	}
	if outer[8] != "sample.png" {
		t.Errorf("expected sample.png, got %v", outer[8])
	}
}

func TestParseUploadedMediaID(t *testing.T) {
	sampleResp := []byte(`)]}'
55
[["wrb.fr","maseQ","[[\"b3b12345-6789-4abc-def0-123456789abc\"]]",null,null,null,"generic"]]`)
	mediaID, err := ParseUploadedMediaID(sampleResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mediaID != "b3b12345-6789-4abc-def0-123456789abc" {
		t.Errorf("expected media ID match, got %s", mediaID)
	}
}

func TestParseUploadedMediaIDUsesDiscoveredRPC(t *testing.T) {
	raw := []byte(`)]}'` + "\n\n1\n" + `[["wrb.fr","runtimeUploadId","[[\"b3b12345-6789-4abc-def0-123456789abc\"]]",null,null,null,"generic"]]`)
	mediaID, err := parseUploadedMediaID(raw, "runtimeUploadId")
	if err != nil || mediaID != "b3b12345-6789-4abc-def0-123456789abc" {
		t.Fatalf("dynamic upload RPC response was not parsed: %q %v", mediaID, err)
	}
}

func TestBuildBatchEnvelope(t *testing.T) {
	inner := []any{"projects/*", 21, nil, nil, nil, nil, []any{1}}
	envelope, err := BuildBatchEnvelope("UpteDb", inner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed []any
	if err := json.Unmarshal([]byte(envelope), &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected outer length 1, got %d", len(parsed))
	}
	l1 := parsed[0].([]any)
	l2 := l1[0].([]any)
	if len(l2) != 4 {
		t.Fatalf("expected l2 length 4, got %d", len(l2))
	}
	if l2[0] != "UpteDb" {
		t.Errorf("expected UpteDb, got %v", l2[0])
	}
	// The second element MUST be a JSON string, not a slice/map
	innerStr, ok := l2[1].(string)
	if !ok {
		t.Fatalf("expected string at index 1, got %T: %v", l2[1], l2[1])
	}
	if innerStr != `["projects/*",21,null,null,null,null,[1]]` {
		t.Errorf("unexpected inner string: %s", innerStr)
	}
	if l2[2] != nil {
		t.Errorf("expected nil at index 2, got %v", l2[2])
	}
	if l2[3] != "generic" {
		t.Errorf("expected generic at index 3, got %v", l2[3])
	}
}

// getModelsPayload follows the captured HTrJv payload, trimmed to what the parser reads.
const getModelsPayload = `[[null,null,` +
	`[[1,[null,null,"beluga_display","veo_3_1_lite"]],[2,[null,null,"nano_banana_pro","abra"]]],` +
	`[["GEM_PIX",["Nano Banana"]]],` +
	`[["Omni 1.1 Flash",[["abra_t2v_4s",null,null,null,null,null,null,null,[[[[1]]]]]],null,"omni"]],` +
	`[["Nano Banana Pro",[["GEM_PIX_2",null,null,null,null,null,null,null,[[[[1]],[[1,4]]]],10]],true,"nano_banana_pro"],` +
	`["Nano Banana 2 Lite",[["HARBOR_SEAL",null,null,null,null,null,null,null,[[[[1]],[[1,3]]]],3]],null,"harbor_seal"],` +
	`["Nano Banana 2.1",[["BELUGA",null,null,null,null,null,null,null,[[[[1]]]]]],null,"beluga_display",[["Nano Banana 2"]]],` +
	`["2K",[["GEM_PIX_2_UPSAMPLE_2K",null,null,null,null,null,null,null,[[[[5]]]]]],true,"upsample_2k"],` +
	`["4K",[["GEM_PIX_2_UPSAMPLE_4K",null,null,null,null,null,null,null,[[[[6]]]]]],true,"upsample_4k"]],` +
	`null,[]]]`

func TestParseImageModels(t *testing.T) {
	list, err := parseImageModels(getModelsPayload, 2)
	if err != nil {
		t.Fatal(err)
	}
	var ids, defaults []string
	for _, m := range list {
		ids = append(ids, m.ID)
		if m.Default {
			defaults = append(defaults, m.ID)
		}
	}
	if strings.Join(ids, ",") != "GEM_PIX_2,HARBOR_SEAL,BELUGA" {
		t.Fatalf("expected image generation models only, got %v", ids)
	}
	if strings.Join(defaults, ",") != "GEM_PIX_2" {
		t.Fatalf("expected the tier 2 default, got %v", defaults)
	}
	if list[0].MaxReferenceImages != 10 || list[1].MaxReferenceImages != 3 || list[2].MaxReferenceImages != 0 {
		t.Fatalf("reference limits not read from the model entry: %+v", list)
	}
	for _, model := range list {
		if got := strings.Join(imageQualityNames(model.AvailableQualities), ","); got != "1K,2K,4K" {
			t.Fatalf("available resolutions not read from Flow's live catalogue: %s", got)
		}
	}

	// An unknown tier uses the first default Flow lists.
	list, err = parseImageModels(getModelsPayload, -1)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list {
		if m.Default != (m.ID == "BELUGA") {
			t.Fatalf("unexpected default for unknown tier: %+v", m)
		}
	}

	for _, broken := range []string{`{}`, `[]`, `[[null,null,[],[],[]]]`, `[[null,null,[],[],[],[]]]`} {
		if _, err := parseImageModels(broken, 1); err == nil {
			t.Errorf("expected error for payload %s", broken)
		}
	}
}

func imageQualityNames(qualities []ImageQuality) []string {
	names := make([]string, len(qualities))
	for i, quality := range qualities {
		names[i] = string(quality)
	}
	return names
}

func TestBuildImageUpsampleRequestUsesFlowResolutionEnums(t *testing.T) {
	projectID := "22d33fe3-5afe-4106-8913-dc663677f8"
	for _, test := range []struct {
		quality ImageQuality
		code    int
	}{{ImageQuality2K, 1}, {ImageQuality4K, 2}} {
		payload, err := BuildImageUpsampleRequest("media-id", test.quality, projectID, "captcha")
		if err != nil {
			t.Fatal(err)
		}
		var got []any
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0] != "media-id" || got[1] != float64(test.code) {
			t.Fatalf("unexpected Flow upsample request for %s: %s", test.quality, payload)
		}
	}
	if _, err := BuildImageUpsampleRequest("media-id", ImageQuality1K, projectID, "captcha"); err == nil {
		t.Fatal("1K must use the original image instead of calling UpsampleImage")
	}
}

func TestParseUpsampledImageBytesAcceptsSupportedImagePayloads(t *testing.T) {
	const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZsAAAAASUVORK5CYII="
	for _, payload := range []string{
		`["` + pngBase64 + `"]`,
		`{"image":{"bytes":"` + pngBase64 + `"}}`,
	} {
		data, contentType, err := ParseUpsampledImageBytes(payload)
		if err != nil || len(data) == 0 || contentType != "image/png" {
			t.Fatalf("unexpected image decode: type=%q bytes=%d error=%v", contentType, len(data), err)
		}
	}
	if _, _, err := ParseUpsampledImageBytes(`{"image":"not-an-image"}`); err == nil {
		t.Fatal("expected invalid image data to be rejected")
	}
}

func TestParseProjectsNewestFirst(t *testing.T) {
	payload := `[[` +
		`["ab977144-867b-4086-9ea0-c340687e3a16",["Older",null,[1791882420,17405000]]],` +
		`["not-a-project",["Ignored",null,[1891882476,0]]],` +
		`["22d33fe3-5afe-4106-8913-dc66336677f8",["Newest",null,[1791882476,407749000],"thumb","ab123739-eaaf-4b2f-aa71-231027ef37ee",[]]]` +
		`]]`
	projects, err := parseProjects(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 || projects[0].ID != "22d33fe3-5afe-4106-8913-dc66336677f8" || projects[0].Title != "Newest" {
		t.Fatalf("unexpected projects: %+v", projects)
	}
	if projects, err := parseProjects(`[]`); err != nil || len(projects) != 0 {
		t.Fatalf("an account without projects must yield none: %v %v", projects, err)
	}
	if _, err := parseProjects(`{`); err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestParseUploadedMediaIDSkipsIDsTheRequestSent(t *testing.T) {
	project := "22d33fe3-5afe-4106-8913-dc66336677f8"
	raw := []byte(`)]}'

99
[["wrb.fr","maseQ","[[\"` + project + `\",\"b3b12345-6789-4abc-def0-123456789abc\"]]",null,null,null,"generic"]]`)
	mediaID, err := ParseUploadedMediaID(raw, strings.ToUpper(project))
	if err != nil {
		t.Fatal(err)
	}
	if mediaID != "b3b12345-6789-4abc-def0-123456789abc" {
		t.Fatalf("picked an ID the request sent: %s", mediaID)
	}

	failed := []byte(`)]}'

60
[["wrb.fr","maseQ",null,null,null,[3],"generic"]]`)
	if _, err := ParseUploadedMediaID(failed); err == nil {
		t.Fatal("expected the provider error to be reported")
	}
}

func TestBuildImageBatchRequestRejectsMissingValues(t *testing.T) {
	for name, build := range map[string]func() ([]byte, error){
		"no project":    func() ([]byte, error) { return BuildImageBatchRequest("p", "", "1:1", "BELUGA", 1, "tok") },
		"no model":      func() ([]byte, error) { return BuildImageBatchRequest("p", "proj", "1:1", "", 1, "tok") },
		"unknown ratio": func() ([]byte, error) { return BuildImageBatchRequest("p", "proj", "2:1", "BELUGA", 1, "tok") },
		"zero count":    func() ([]byte, error) { return BuildImageBatchRequest("p", "proj", "1:1", "BELUGA", 0, "tok") },
		"upload no mime": func() ([]byte, error) {
			return BuildImageUploadRequest("AAAA", "", "a.png", "proj", "tok")
		},
		"upload no project": func() ([]byte, error) {
			return BuildImageUploadRequest("AAAA", "image/png", "a.png", "", "tok")
		},
	} {
		if _, err := build(); err == nil {
			t.Errorf("%s: expected an error instead of a default value", name)
		}
	}
}
