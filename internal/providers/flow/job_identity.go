package flow

import (
	"encoding/json"
	"strings"
)

func sameEditRequest(a, b EditVideoRequest) bool {
	if strings.TrimSpace(a.Prompt) != strings.TrimSpace(b.Prompt) || a.ModelID != b.ModelID || a.SourceID != b.SourceID || a.TrimStartMS != b.TrimStartMS || a.TrimEndMS != b.TrimEndMS {
		return false
	}
	return a.SourceID != "" || (a.SourceURL == b.SourceURL && a.ProjectID == b.ProjectID)
}

// Workflow metadata carries [title, timestamp, ..., media ID, client seed].
// Matching the submitted seed avoids adopting an older version of a workflow
// when a mutation response was lost after reaching Google's server.
func mediaForClient(payload, clientID string) pendingVideo {
	if matches := mediaForSeed(payload, clientID); len(matches) > 0 {
		return matches[0]
	}
	return pendingVideo{}
}

// mediaForSeed lists every workflow carrying seed. Flow records a video
// request's batch ID (the last tuple of the request) as the seed of each of
// its workflows, so one seed can name several videos.
func mediaForSeed(payload, seed string) []pendingVideo {
	var root []any
	if seed == "" || json.Unmarshal([]byte(payload), &root) != nil || len(root) < 2 {
		return nil
	}
	var matches []pendingVideo
	for _, value := range asList(root[1]) {
		row := asList(value)
		if len(row) < 4 {
			continue
		}
		metadata := asList(row[3])
		if len(metadata) < 6 {
			continue
		}
		if found, _ := metadata[5].(string); !strings.EqualFold(found, seed) {
			continue
		}
		name, _ := metadata[4].(string)
		workflow, _ := row[0].(string)
		matches = append(matches, pendingVideo{name: name, operation: workflow, clientID: seed})
	}
	return matches
}
