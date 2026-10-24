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
	var root []any
	if json.Unmarshal([]byte(payload), &root) != nil || len(root) < 2 {
		return pendingVideo{}
	}
	for _, value := range asList(root[1]) {
		row := asList(value)
		if len(row) < 4 {
			continue
		}
		metadata := asList(row[3])
		if len(metadata) < 6 {
			continue
		}
		seed, _ := metadata[5].(string)
		if !strings.EqualFold(seed, clientID) {
			continue
		}
		name, _ := metadata[4].(string)
		workflow, _ := row[0].(string)
		return pendingVideo{name: name, operation: workflow, clientID: clientID}
	}
	return pendingVideo{}
}
