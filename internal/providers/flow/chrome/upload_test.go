package chrome

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
)

const projectUUID = "22d33fe3-5afe-4106-8913-dc66336677f8"

// uploadReply is the completion record from the live capture.
const uploadReply = `{"media":{"name":"projects/22d33fe3-5afe-4106-8913-dc66336677f8/media/8eb41f86-bae8-4452-b5bb-39406e6343ee","video":{"dimensions":{"width":1280,"height":720}}},"mediaId":"8eb41f86-bae8-4452-b5bb-39406e6343ee","workflow":{"name":"projects/22d33fe3-5afe-4106-8913-dc66336677f8/workflows/8eb41f86-bae8-4452-b5bb-39406e6343ee","metadata":{"displayName":"sample_reference_video.mp4"}}}`

func TestUploadVideoUsesResumableProtocol(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	dir := t.TempDir()
	file := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(file, []byte("MP4DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := flowTestCredentials()
	c.Cookies = append(c.Cookies, CookieItem{Name: xsrfCookie, Value: "xsrf-value", Domain: "flow.google.com", Path: "/", Secure: true})

	var steps []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := r.Header
		if h.Get("X-Framework-Xsrf-Token") != "xsrf-value" || h.Get("Slug") != "my%20clip%20(1).mp4" || h.Get("x-trim-start") != "500" || h.Get("x-trim-end") != "8500" {
			t.Errorf("upload headers missing: %v", h)
		}
		if strings.Contains(h.Get("Cookie"), "must-not-send") {
			t.Errorf("cookie scope ignored: %s", h.Get("Cookie"))
		}
		switch h.Get("X-Goog-Upload-Command") {
		case "start":
			steps = append(steps, "start")
			if r.URL.String() != FlowURL+videoUploadPath+projectUUID || h.Get("X-Goog-Upload-Protocol") != "resumable" || h.Get("X-Goog-Upload-Header-Content-Length") != "7" {
				t.Errorf("unexpected start request %s %v", r.URL, h)
			}
			header := http.Header{"X-Goog-Upload-Status": {"active"}, "X-Goog-Upload-Url": {FlowURL + videoUploadPath + projectUUID + "?upload_id=abc&upload_protocol=resumable"}}
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		case "upload, finalize":
			steps = append(steps, "upload")
			body, _ := io.ReadAll(r.Body)
			if string(body) != "MP4DATA" || h.Get("X-Goog-Upload-Offset") != "0" || r.URL.Query().Get("upload_id") != "abc" {
				t.Errorf("unexpected upload request %s %q %v", r.URL, body, h)
			}
			header := http.Header{"X-Goog-Upload-Status": {"final"}}
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(uploadReply)), Request: r}, nil
		}
		t.Fatalf("unexpected command %q", h.Get("X-Goog-Upload-Command"))
		return nil, nil
	})

	client, err := NewFlowClient(c, config.DefaultFlowPagePolicy())
	if err != nil {
		t.Fatal(err)
	}
	video, err := client.UploadVideo(context.Background(), projectUUID, file, "my clip (1).mp4", "video/mp4", &VideoTrim{StartMS: 500, EndMS: 8500}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(steps, ",") != "start,upload" {
		t.Fatalf("steps = %v", steps)
	}
	want := UploadedVideo{MediaID: "8eb41f86-bae8-4452-b5bb-39406e6343ee", WorkflowID: "8eb41f86-bae8-4452-b5bb-39406e6343ee", DisplayName: "sample_reference_video.mp4", Width: 1280, Height: 720}
	if video != want {
		t.Fatalf("video = %+v", video)
	}
}

func TestUploadVideoStopsOnClientError(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	file := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	client, err := NewFlowClient(flowTestCredentials(), config.DefaultFlowPagePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.UploadVideo(context.Background(), projectUUID, file, "clip.mp4", "video/mp4", nil, time.Second); err == nil || calls != 1 {
		t.Fatalf("a 403 must fail without retry: %v after %d calls", err, calls)
	}
}

func TestUploadTargetStaysOnGoogle(t *testing.T) {
	client, err := NewFlowClient(flowTestCredentials(), config.DefaultFlowPagePolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "http://flow.google.com/x", "https://evil.example.com/x", "https://user@flow.google.com/x"} {
		if _, err := client.uploadTarget(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got, err := client.uploadTarget("/upload/v1/x?upload_id=1"); err != nil || got != FlowURL+"/upload/v1/x?upload_id=1" {
		t.Errorf("relative target: %q %v", got, err)
	}
}

func TestParseUploadedVideoFallbacks(t *testing.T) {
	video, err := ParseUploadedVideo([]byte(`{"media":{"name":"projects/p/media/abc"},"workflow":{"metadata":{"display_name":"a%20b.mp4"}}}`), "orig.mp4")
	if err != nil || video.MediaID != "abc" || video.WorkflowID != "abc" || video.DisplayName != "a b.mp4" {
		t.Fatalf("unexpected %+v %v", video, err)
	}
	if _, err := ParseUploadedVideo([]byte(`{}`), "x"); err == nil {
		t.Fatal("a reply without media must fail")
	}
}
