package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type textsIn struct {
	Text Texts `json:"text"`
}

type imagesOut struct {
	Images []MediaRef `json:"images"`
}

type mediaIn struct {
	Media []MediaRef `json:"media"`
}

type countOut struct {
	Count int `json:"count"`
}

// makeImages turns each text into one stored image.
func makeImages(t string, calls *[]string) Node {
	return Typed(Spec{
		Type:    t,
		Inputs:  []Port{{Name: "text", Type: PortText, Multiple: true}},
		Outputs: []Port{{Name: "images", Type: PortImage, Multiple: true}},
	}, func(_ context.Context, in textsIn) (imagesOut, error) {
		*calls = append(*calls, t)
		out := imagesOut{}
		for _, s := range in.Text.NonEmpty() {
			out.Images = append(out.Images, RefFor("/flow-media/"+s+".png", MediaImage))
		}
		return out, nil
	})
}

func countMedia(t string, port PortType, calls *[]string) Node {
	return Typed(Spec{
		Type:    t,
		Inputs:  []Port{{Name: "media", Type: port, Multiple: true}},
		Outputs: []Port{{Name: "count", Type: PortText}},
	}, func(_ context.Context, in mediaIn) (countOut, error) {
		*calls = append(*calls, t)
		return countOut{Count: len(in.Media)}, nil
	})
}

func TestCompatible(t *testing.T) {
	cases := []struct {
		from, to PortType
		want     bool
	}{
		{PortImage, PortImage, true}, {PortImage, PortMedia, true}, {PortVideo, PortMedia, true},
		{PortMedia, PortImage, false}, {PortImage, PortVideo, false}, {PortText, PortImage, false},
		{PortMedia, PortMedia, true}, {PortText, PortText, true},
	}
	for _, c := range cases {
		if got := Compatible(c.from, c.to); got != c.want {
			t.Errorf("Compatible(%s, %s) = %v", c.from, c.to, got)
		}
	}
	if RefFor("/flow-media/a.mp4", MediaImage).Kind != MediaVideo || RefFor("/flow-media/a.png", MediaVideo).Kind != MediaImage {
		t.Fatal("the extension decides the kind")
	}
}

func TestGraphRunsInDependencyOrder(t *testing.T) {
	var calls []string
	r := NewRegistry()
	if err := r.Register(makeImages("make", &calls), countMedia("count", PortMedia, &calls)); err != nil {
		t.Fatal(err)
	}
	g := Graph{
		// Listed downstream first: order must follow the wiring.
		Nodes: []GraphNode{
			{ID: "c", Type: "count"},
			{ID: "a", Type: "make", Params: json.RawMessage(`{"text":["x","y"]}`)},
			{ID: "b", Type: "make", Params: json.RawMessage(`{"text":"z"}`)},
		},
		Edges: []Edge{
			{From: Endpoint{"a", "images"}, To: Endpoint{"c", "media"}},
			{From: Endpoint{"b", "images"}, To: Endpoint{"c", "media"}},
		},
	}
	res, err := r.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Order, ",") != "a,b,c" || !res.Done() {
		t.Fatalf("unexpected run %+v", res)
	}
	var out countOut
	_ = json.Unmarshal(res.Nodes["c"].Output, &out)
	if out.Count != 3 {
		t.Fatalf("both lists must reach the multiple input, got %d", out.Count)
	}
}

func TestGraphRefusesBadWiring(t *testing.T) {
	var calls []string
	r := NewRegistry()
	if err := r.Register(makeImages("make", &calls), countMedia("videos", PortVideo, &calls)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]Graph{
		"cycle": {
			Nodes: []GraphNode{{ID: "a", Type: "make"}, {ID: "b", Type: "make"}},
			Edges: []Edge{{From: Endpoint{"a", "images"}, To: Endpoint{"b", "text"}}, {From: Endpoint{"b", "images"}, To: Endpoint{"a", "text"}}},
		},
		"incompatible": {
			Nodes: []GraphNode{{ID: "a", Type: "make", Params: json.RawMessage(`{"text":"x"}`)}, {ID: "b", Type: "videos"}},
			Edges: []Edge{{From: Endpoint{"a", "images"}, To: Endpoint{"b", "media"}}},
		},
		"unknown port": {
			Nodes: []GraphNode{{ID: "a", Type: "make", Params: json.RawMessage(`{"text":"x"}`)}, {ID: "b", Type: "make"}},
			Edges: []Edge{{From: Endpoint{"a", "files"}, To: Endpoint{"b", "text"}}},
		},
		"empty input": {Nodes: []GraphNode{{ID: "a", Type: "make"}}},
	}
	for name, g := range cases {
		if _, err := r.RunGraph(context.Background(), g); err == nil {
			t.Errorf("%s: graph must be refused", name)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("nothing may run on an invalid graph, ran %v", calls)
	}
}

func TestPendingStopsDownstream(t *testing.T) {
	var calls []string
	r := NewRegistry()
	pending := Typed(Spec{
		Type:    "slow",
		Outputs: []Port{{Name: "images", Type: PortImage, Multiple: true}},
	}, func(context.Context, struct{}) (imagesOut, error) {
		calls = append(calls, "slow")
		return imagesOut{}, Pending("Flow vẫn đang tạo.", []string{"job-1"})
	})
	if err := r.Register(pending, countMedia("count", PortMedia, &calls), makeImages("make", &calls)); err != nil {
		t.Fatal(err)
	}
	res, err := r.RunGraph(context.Background(), Graph{
		Nodes: []GraphNode{{ID: "s", Type: "slow"}, {ID: "c", Type: "count"}, {ID: "m", Type: "make", Params: json.RawMessage(`{"text":"x"}`)}},
		Edges: []Edge{{From: Endpoint{"s", "images"}, To: Endpoint{"c", "media"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run := res.Nodes["s"]; run.Status != RunPending || len(run.JobIDs) != 1 {
		t.Fatalf("pending node: %+v", run)
	}
	if res.Nodes["c"].Status != RunSkipped || res.Nodes["m"].Status != RunDone {
		t.Fatalf("downstream must be skipped, independent nodes run: %+v", res.Nodes)
	}
	if strings.Contains(strings.Join(calls, ","), "count") {
		t.Fatal("a node after a pending one must not run")
	}
	if Respond[int](0, Pending("x", nil)).Success {
		t.Fatal("pending is not a success")
	}
}

func TestGraphCancellation(t *testing.T) {
	var calls []string
	ctx, cancel := context.WithCancel(context.Background())
	r := NewRegistry()
	stop := Typed(Spec{
		Type:    "stop",
		Outputs: []Port{{Name: "images", Type: PortImage, Multiple: true}},
	}, func(ctx context.Context, _ struct{}) (imagesOut, error) {
		cancel()
		return imagesOut{}, ctx.Err()
	})
	if err := r.Register(stop, makeImages("make", &calls)); err != nil {
		t.Fatal(err)
	}
	res, err := r.RunGraph(ctx, Graph{Nodes: []GraphNode{
		{ID: "s", Type: "stop"}, {ID: "m", Type: "make", Params: json.RawMessage(`{"text":"x"}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Nodes["s"].Status != RunCancelled || res.Nodes["m"].Status != RunCancelled || len(calls) != 0 {
		t.Fatalf("cancelled graph ran on: %+v %v", res.Nodes, calls)
	}
}

func TestWrongMediaKindIsRefused(t *testing.T) {
	var calls []string
	r := NewRegistry()
	video := Typed(Spec{
		Type:    "video",
		Outputs: []Port{{Name: "files", Type: PortMedia, Multiple: true}},
	}, func(context.Context, struct{}) (struct {
		Files []MediaRef `json:"files"`
	}, error) {
		return struct {
			Files []MediaRef `json:"files"`
		}{Files: []MediaRef{RefFor("/flow-media/a.mp4", MediaVideo)}}, nil
	})
	if err := r.Register(video, countMedia("images", PortMedia, &calls)); err != nil {
		t.Fatal(err)
	}
	res, err := r.RunGraph(context.Background(), Graph{
		Nodes: []GraphNode{{ID: "v", Type: "video"}, {ID: "i", Type: "images"}},
		Edges: []Edge{{From: Endpoint{"v", "files"}, To: Endpoint{"i", "media"}}},
	})
	if err != nil || res.Nodes["i"].Status != RunDone {
		t.Fatalf("a video may feed a media input: %+v %v", res, err)
	}
	if err := checkMedia(Port{Name: "refs", Type: PortImage}, []json.RawMessage{json.RawMessage(`{"localUrl":"/flow-media/a.mp4","kind":"video"}`)}); err == nil {
		t.Fatal("a video must not reach an image input")
	}
}

func TestOptionSourcesAreChecked(t *testing.T) {
	r := NewRegistry()
	node := Typed(Spec{Type: "pick", Params: []Param{{Name: "model", Kind: ParamSelect, OptionsFrom: "test.models"}}},
		func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })
	if err := r.Register(node); err != nil {
		t.Fatal(err)
	}
	if err := r.Check(); err == nil {
		t.Fatal("an unresolved option source must be reported")
	}
	err := r.RegisterOptions(map[string]OptionSource{"test.models": func(_ context.Context, sel map[string]string) ([]Option, error) {
		return []Option{{Value: "m1", Label: "M1 " + sel["mode"]}}, nil
	}})
	if err != nil || r.Check() != nil {
		t.Fatalf("source registered: %v", err)
	}
	opts, err := r.Options(context.Background(), "test.models", map[string]string{"mode": "fast"})
	if err != nil || len(opts) != 1 || opts[0].Label != "M1 fast" {
		t.Fatalf("options %+v %v", opts, err)
	}
	if _, err := r.Options(context.Background(), "missing", nil); err == nil {
		t.Fatal("unknown source must fail")
	}
	var engineErr *Error
	if _, err := r.Options(context.Background(), "missing", nil); !errors.As(err, &engineErr) {
		t.Fatal("unknown source is an invalid request")
	}
}
