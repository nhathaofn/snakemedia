package flow

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"testing"
)

// The payload readers below all walk decoded batchexecute JSON, including
// strings that hold nested JSON. These cases pin their answers so the shared
// walker keeps them.

func TestPayloadReadersFollowNestedJSON(t *testing.T) {
	img := "https://flow-content.google/image/abc-1?sig=x"
	vid := "https://flow-content.google/video/v-1?sig=y"
	inner, _ := json.Marshal([]any{[]any{nil, img}, map[string]any{"displayName": "photo.png"}})
	payload, _ := json.Marshal([]any{string(inner), []any{"  " + img + "  "}, map[string]any{"x": []any{vid}}})

	if got := flowProjectImageURLs(string(payload)); !slices.Equal(got, []string{img}) {
		t.Errorf("image URLs %v", got)
	}
	if !flowProjectContainsVideo(string(payload)) {
		t.Error("nested video URL not found")
	}
	if flowProjectContainsVideo(`[["` + img + `"]]`) {
		t.Error("an image URL is not a video")
	}
	if name := flowProjectMediaName("m", string(payload)); name != "photo.png" {
		t.Errorf("display name %q", name)
	}
	if name := flowProjectMediaName("projects/p/media/m", `[]`); name != "m" {
		t.Errorf("fallback name %q", name)
	}
	items := ParseGeneratedImages(string(payload), "p", "model", "1:1")
	if len(items) != 1 || items[0].ID != "abc-1" || items[0].URL != img {
		t.Errorf("generated images %+v", items)
	}
}

func TestProjectMediaMarkersAndOperations(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	cases := []struct {
		name      string
		value     any
		marker    bool
		operation bool
	}{
		{"marker key", map[string]any{"generated_image": map[string]any{}}, true, false},
		{"marker nested in string", []any{`{"generatedImage":{}}`}, true, false},
		{"list operation at 2", []any{id, nil, "op-1"}, false, true},
		{"list operation at 5", []any{nil, nil, nil, nil, id, "op-2"}, false, true},
		{"map operation", map[string]any{"operationId": "op", "mediaId": id}, false, true},
		{"map operation in string", []any{`{"workflow_id":"w","name":"` + id + `"}`}, false, true},
		{"nothing", []any{"a", 1.0, map[string]any{"name": "x"}}, false, false},
	}
	for _, c := range cases {
		if got := projectMediaHasMarker(c.value, "generatedimage"); got != c.marker {
			t.Errorf("%s: marker %v", c.name, got)
		}
		if got := projectMediaHasOperation(c.value); got != c.operation {
			t.Errorf("%s: operation %v", c.name, got)
		}
	}
}

func TestUpsampledBytesFoundAtAnyDepth(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	nums := make([]any, len(png))
	for i, b := range png {
		nums[i] = float64(b)
	}
	b64 := base64.StdEncoding.EncodeToString(png)
	nested, _ := json.Marshal([]any{[]any{"data:image/png;base64," + b64}})
	for name, payload := range map[string]any{
		"numbers":       []any{nil, []any{nums}},
		"base64 nested": []any{string(nested)},
		"base64 in map": map[string]any{"k": b64},
	} {
		raw, _ := json.Marshal(payload)
		data, kind, err := ParseUpsampledImageBytes(string(raw), 1<<20)
		if err != nil || kind != "image/png" || !slices.Equal(data, png) {
			t.Errorf("%s: %v %q %v", name, data, kind, err)
		}
	}
	if _, _, err := ParseUpsampledImageBytes(`[["nothing"]]`, 1<<20); err == nil {
		t.Error("no image must be an error")
	}
}
