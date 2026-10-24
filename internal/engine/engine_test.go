package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"SnakeMedia/internal/models"
)

type echoIn struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

func echoNode(t string) Node {
	return Typed(Spec{Type: t, Provider: "test"}, func(_ context.Context, in echoIn) ([]string, error) {
		if in.Count < 1 {
			return nil, Invalid("count must be positive")
		}
		out := make([]string, in.Count)
		for i := range out {
			out[i] = in.Text
		}
		return out, nil
	})
}

func TestRegistryRunsTypedNodes(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(echoNode("b.echo"), echoNode("a.echo")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(echoNode("a.echo")); err == nil {
		t.Fatal("a node type registered twice must be refused")
	}
	if specs := r.Specs(); len(specs) != 2 || specs[0].Type != "a.echo" {
		t.Fatalf("specs not listed in order: %+v", specs)
	}

	out, err := r.Run(context.Background(), "a.echo", json.RawMessage(`{"text":"hi","count":2}`))
	if err != nil || len(out.([]string)) != 2 {
		t.Fatalf("unexpected result %v %v", out, err)
	}
	for name, input := range map[string]string{"bad json": `{`, "invalid": `{"count":0}`} {
		_, err := r.Run(context.Background(), "a.echo", json.RawMessage(input))
		if res := Respond[any](nil, err); res.Success || res.Code != models.CodeInvalidArgument {
			t.Errorf("%s: expected CodeInvalidArgument, got %+v", name, res)
		}
	}
	if _, err := r.Run(context.Background(), "missing", nil); err == nil {
		t.Fatal("an unknown node type must fail")
	}
}

func TestResponseRoundTripKeepsCodes(t *testing.T) {
	out, err := FromResponse(models.ErrorResponse[int]("down", models.CodeUnavailable))
	if res := Respond(out, err); res.Success || res.Code != models.CodeUnavailable || res.Error != "down" {
		t.Fatalf("code lost: %+v", res)
	}
	if res := Respond(7, nil); !res.Success || res.Data != 7 {
		t.Fatalf("success lost: %+v", res)
	}
	if res := Respond[int](0, errors.New("boom")); res.Code != models.CodeInternal {
		t.Fatalf("plain errors are internal: %+v", res)
	}
}
