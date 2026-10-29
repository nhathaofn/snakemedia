package flow

import (
	"encoding/json"
	"testing"
)

func TestVideoRequestSendsFirstClientIDAsBatchID(t *testing.T) {
	inner, err := BuildVideoTextRequestWithClientIDs("boat", "project", "abra_t2v_4s_360p", 1, videoResolution720p, 2, "token", []string{"aaaa-1", "bbbb-2"})
	if err != nil {
		t.Fatal(err)
	}
	var root []any
	if err := json.Unmarshal(inner, &root); err != nil || len(root) != 3 {
		t.Fatalf("request shape: %s %v", inner, err)
	}
	if batch := asList(root[2]); len(batch) != 2 || batch[0] != "AAAA-1" {
		t.Fatalf("batch tuple = %v, want the first client ID", root[2])
	}
}

func TestReconcileAdoptsVideosByBatchSeed(t *testing.T) {
	// Shape of GetProjectContents for build boq_labs-ai-sandbox-frontend_20261028:
	// both workflows of one request carry the request's batch ID as seed.
	listing := `[null,[` +
		`["wf-old",null,null,["Old",[1,0],null,null,"media-old","OTHER-SEED",[1,0]],"p"],` +
		`["wf-1",null,null,["Boat",[2,0],null,null,"media-1","AAAA-1",[2,0]],"p"],` +
		`["wf-2",null,null,["Boat",[2,0],null,null,"media-2","AAAA-1",[2,0]],"p"]` +
		`]]`
	known := map[string]bool{}
	started := adoptBatchVideos(listing, []string{"aaaa-1", "bbbb-2"}, nil, known)
	if len(started) != 2 || !known["aaaa-1"] || !known["bbbb-2"] {
		t.Fatalf("adopted %+v known %v", started, known)
	}
	if started[0].name != "media-1" || started[1].name != "media-2" || started[1].clientID != "bbbb-2" {
		t.Fatalf("adopted %+v", started)
	}
	// A video already matched is not handed to a second client ID.
	known = map[string]bool{"aaaa-1": true}
	started = adoptBatchVideos(listing, []string{"aaaa-1", "bbbb-2"}, []pendingVideo{{name: "media-1", operation: "wf-1", clientID: "aaaa-1"}}, known)
	if len(started) != 2 || started[1].name != "media-2" {
		t.Fatalf("adopted %+v", started)
	}
	// Nothing carries the seed: nothing is adopted.
	if got := adoptBatchVideos(listing, []string{"cccc-3"}, nil, map[string]bool{}); len(got) != 0 {
		t.Fatalf("adopted %+v", got)
	}
}
