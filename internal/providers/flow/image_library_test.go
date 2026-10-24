package flow

import (
	"testing"
	"time"
)

func TestFlowProjectImageURLs(t *testing.T) {
	payload := `[["https://flow-content.google/image/image-1?sig=abc","https://example.com/image/image-2"]]`
	urls := flowProjectImageURLs(payload)
	if len(urls) != 1 || urls[0] != "https://flow-content.google/image/image-1?sig=abc" {
		t.Fatalf("unexpected project image URLs: %#v", urls)
	}
}

func TestFlowProjectContainsVideoAndDisplayName(t *testing.T) {
	payload := `{"media":{"displayName":"reference-photo.png","thumbnail":"https://flow-content.google/image/thumb?sig=a","source":"https://flow-content.google/video/clip?sig=b"}}`
	if !flowProjectContainsVideo(payload) {
		t.Fatal("video media was not detected")
	}
	if name := flowProjectMediaName("media-id", payload); name != "reference-photo.png" {
		t.Fatalf("unexpected project image name %q", name)
	}
}

func TestParseImageProjectTitle(t *testing.T) {
	provider := New(nil, nil, nil, "SnakeMedia")
	now := time.Date(2026, time.October, 24, 13, 41, 20, 0, time.Local)

	mode, createdAt, ok := provider.parseImageProjectTitle("SnakeMedia - All - 24/10 - 13:41:20", now)
	if !ok || mode != ImageProjectModeAll {
		t.Fatalf("expected today's All project title to parse, got mode=%q ok=%t", mode, ok)
	}
	if got := createdAt.Format("2006-01-02 15:04:05"); got != "2026-10-24 13:41:20" {
		t.Fatalf("unexpected parsed creation time %q", got)
	}

	if _, _, ok := provider.parseImageProjectTitle("SnakeMedia - Single - 24/10 - 13:41", now); ok {
		t.Fatal("title without seconds should not parse")
	}
}

func TestFindDailyImageProjectReturnsOnlyRequestedMode(t *testing.T) {
	provider := New(nil, nil, nil, "SnakeMedia")
	now := time.Date(2026, time.October, 24, 13, 41, 20, 0, time.Local)
	projects := []flowProject{
		{ID: "single-project", Title: "SnakeMedia - Single - 24/10 - 13:40:00"},
		{ID: "all-project", Title: "SnakeMedia - All - 24/10 - 13:41:00"},
	}

	for _, tc := range []struct {
		mode ImageProjectMode
		id   string
	}{
		{mode: ImageProjectModeSingle, id: "single-project"},
		{mode: ImageProjectModeAll, id: "all-project"},
	} {
		project, found := provider.findDailyImageProject(projects, tc.mode, now)
		if !found || project.ID != tc.id {
			t.Fatalf("findDailyImageProject(%q)=(%q, %t); want (%q, true)", tc.mode, project.ID, found, tc.id)
		}
	}
}

func TestGeneratedImageMediaCountExcludesUploads(t *testing.T) {
	payload := `[null,null,[
		["b3b12345-6789-4abc-def0-123456789abc", "project", "operation-1", null, null, null],
		["Generated result", [1, 0], false, null, "c4c23456-789a-4bcd-ef01-23456789abcd", "operation-2", true],
		["uploaded-reference", "project", null, null, null, null],
		{"image":{"generatedImage":{"name":"generated-image"}}},
		{"image":{"userUploadedImage":{"name":"reference-image","operationId":"upload-operation"}}}
	]]`
	count, err := generatedImageMediaCount(payload)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("generatedImageMediaCount()=%d; want 3", count)
	}
}

func TestShouldRotateImageProjectBeforeRequestWouldExceedLimit(t *testing.T) {
	cases := []struct {
		name      string
		generated int
		requested int
		want      bool
	}{
		{name: "exactly reaches limit", generated: 99, requested: 1, want: false},
		{name: "next image would cross full project", generated: 100, requested: 1, want: true},
		{name: "crosses limit", generated: 99, requested: 2, want: true},
		{name: "already above limit", generated: 101, requested: 1, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRotateImageProject(tc.generated, tc.requested); got != tc.want {
				t.Fatalf("shouldRotateImageProject(%d, %d)=%t; want %t", tc.generated, tc.requested, got, tc.want)
			}
		})
	}
}

func TestIsRecentImageProjectIncludesThreeCalendarDays(t *testing.T) {
	now := time.Date(2026, time.October, 24, 13, 0, 0, 0, time.Local)
	cases := []struct {
		name string
		day  int
		want bool
	}{
		{name: "today", day: 24, want: true},
		{name: "yesterday", day: 23, want: true},
		{name: "two days ago", day: 22, want: true},
		{name: "older than three calendar days", day: 21, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			createdAt := time.Date(2026, time.October, tc.day, 12, 0, 0, 0, time.Local)
			if got := isRecentImageProject(createdAt, now); got != tc.want {
				t.Fatalf("isRecentImageProject(%s)=%t; want %t", createdAt, got, tc.want)
			}
		})
	}
}
