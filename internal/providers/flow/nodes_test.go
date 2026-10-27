package flow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/muse"
)

// pngBytes is a minimal PNG header, enough for content sniffing.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// TestMuseImageFeedsFlowVideo wires a node shaped like muse.image.generate
// (its NodeResult) into flow.video.generate: Flow must receive the image's
// bytes as a reference and hand its video on as a media reference.
func TestMuseImageFeedsFlowVideo(t *testing.T) {
	store := media.NewStore(t.TempDir())
	image, err := store.Put("muse-out", "image/png", ".png", pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	var got GenerateVideoRequest
	runners := nodeRunners{
		media: store,
		videos: func(_ context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
			got = req
			return models.SuccessResponse([]GeneratedVideoItem{{JobID: "r1", LocalURL: "/flow-media/v1.mp4"}})
		},
	}
	museLike := engine.Typed(engine.Spec{
		Type:    muse.NodeImageGenerate,
		Outputs: []engine.Port{{Name: "files", Type: engine.PortImage, Multiple: true}},
	}, func(context.Context, struct{}) (muse.NodeResult, error) {
		return muse.NodeResult{Files: []engine.MediaRef{engine.RefFor(image, engine.MediaImage)}, Failed: []string{}}, nil
	})
	reg := engine.NewRegistry()
	if err := reg.Register(append(flowNodes("flow", runners), museLike)...); err != nil {
		t.Fatal(err)
	}
	res, err := reg.RunGraph(context.Background(), engine.Graph{
		Nodes: []engine.GraphNode{
			{ID: "m", Type: muse.NodeImageGenerate},
			{ID: "v", Type: NodeVideoGenerate, Params: json.RawMessage(`{"prompt":"pan slowly","modelId":"veo","count":1}`)},
		},
		Edges: []engine.Edge{{From: engine.Endpoint{Node: "m", Port: "files"}, To: engine.Endpoint{Node: "v", Port: "references"}}},
	})
	if err != nil || !res.Done() {
		t.Fatalf("graph did not finish: %+v %v", res, err)
	}
	if len(got.References) != 1 || got.Prompt != "pan slowly" || got.References[0].MimeType != "image/png" {
		t.Fatalf("Flow got the wrong request: %+v", got)
	}
	if data, _ := base64.StdEncoding.DecodeString(got.References[0].Base64); string(data) != string(pngBytes) {
		t.Fatal("Flow must receive the Muse image's bytes")
	}
	var out VideoNodeOutput
	_ = json.Unmarshal(res.Nodes["v"].Output, &out)
	if len(out.Videos) != 1 || out.Videos[0].Kind != engine.MediaVideo {
		t.Fatalf("videos handed on as media references: %+v", out)
	}
}

func TestFlowNodePendingIsNotSuccess(t *testing.T) {
	calls := 0
	runners := nodeRunners{
		images: func(_ context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
			calls++
			return models.SuccessResponse([]GeneratedImageItem{{JobID: "req-" + req.Prompt, Pending: true}})
		},
	}
	reg := engine.NewRegistry()
	if err := reg.Register(flowNodes("flow", runners)...); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Run(context.Background(), NodeImageGenerate, json.RawMessage(`{"prompt":["a","b"]}`))
	pending, ok := err.(*engine.PendingError)
	if !ok || len(pending.JobIDs) != 2 || calls != 2 {
		t.Fatalf("pending images must stop the node with their jobs: %+v %v (calls %d)", out, err, calls)
	}
}

func TestFlowNodeRefusesFilesOutsideMedia(t *testing.T) {
	runners := nodeRunners{media: media.NewStore(t.TempDir())}
	if _, err := runners.references([]engine.MediaRef{{LocalURL: `C:\Windows\win.ini`, Kind: engine.MediaImage}}); err == nil {
		t.Fatal("a reference outside the media store must be refused")
	}
	if _, err := runners.references([]engine.MediaRef{{LocalURL: "/flow-media/a.mp4", Kind: engine.MediaVideo}}); err == nil {
		t.Fatal("a video is not a reference image")
	}
}

func TestPickModelUsesSelectionThenDefault(t *testing.T) {
	list := []FlowVideoModel{{ID: "a"}, {ID: "b", Default: true}}
	key := func(m FlowVideoModel) (string, bool) { return m.ID, m.Default }
	if m, _ := pickModel(list, "", key); m.ID != "b" {
		t.Fatalf("default expected, got %s", m.ID)
	}
	if m, _ := pickModel(list, "a", key); m.ID != "a" {
		t.Fatalf("selection expected, got %s", m.ID)
	}
	if _, err := pickModel(list, "zz", key); err == nil {
		t.Fatal("an unknown model must fail")
	}
}

func TestEveryFlowOptionSourceIsRegistered(t *testing.T) {
	reg := engine.NewRegistry()
	if err := reg.Register(flowNodes("flow", nodeRunners{})...); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterOptions((&Provider{}).OptionSources()); err != nil {
		t.Fatal(err)
	}
	if err := reg.Check(); err != nil {
		t.Fatal(err)
	}
}

// TestGraphRerunReusesFlowRequestIDs runs the same graph twice under one
// RequestID: both runs must send the same request IDs (so the second resumes
// through the journal), a definitely refused ID moves to the next attempt, and
// a run without a RequestID leaves the ID to the provider.
func TestGraphRerunReusesFlowRequestIDs(t *testing.T) {
	var ids []string
	rejected := map[string]bool{}
	runners := nodeRunners{
		images: func(_ context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
			ids = append(ids, req.RequestID)
			return models.SuccessResponse([]GeneratedImageItem{{JobID: req.RequestID, LocalURL: "/flow-media/" + req.Prompt + ".png"}})
		},
		rejected: func(kind, id string) bool { return kind == generationJobImage && rejected[id] },
	}
	reg := engine.NewRegistry()
	if err := reg.Register(flowNodes("flow", runners)...); err != nil {
		t.Fatal(err)
	}
	graph := engine.Graph{
		RequestID: "run-1",
		Nodes:     []engine.GraphNode{{ID: "i", Type: NodeImageGenerate, Params: json.RawMessage(`{"prompt":["a","b"],"modelId":"m","count":1}`)}},
	}
	run := func(g engine.Graph) []string {
		ids = nil
		if res, err := reg.RunGraph(context.Background(), g); err != nil || !res.Done() {
			t.Fatalf("graph did not finish: %+v %v", res, err)
		}
		return append([]string(nil), ids...)
	}
	first := run(graph)
	if len(first) != 2 || first[0] == "" || first[0] == first[1] {
		t.Fatalf("each prompt needs its own stable ID: %v", first)
	}
	if again := run(graph); again[0] != first[0] || again[1] != first[1] {
		t.Fatalf("a re-run must reuse the IDs: %v vs %v", again, first)
	}
	rejected[first[0]] = true
	if retry := run(graph); retry[0] == first[0] || retry[1] != first[1] {
		t.Fatalf("only the refused send gets a new ID: %v vs %v", retry, first)
	}
	if other := run(engine.Graph{Nodes: graph.Nodes}); other[0] != "" || other[1] != "" {
		t.Fatalf("without a RequestID the provider picks the IDs: %v", other)
	}
}
