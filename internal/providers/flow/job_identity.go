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
	return parseWorkflows(payload).forClient(clientID)
}

// mediaForSeed lists every workflow carrying seed. Flow records a video
// request's batch ID (the last tuple of the request) as the seed of each of
// its workflows, so one seed can name several videos.
func mediaForSeed(payload, seed string) []pendingVideo {
	if seed == "" {
		return nil
	}
	return parseWorkflows(payload).forSeed(seed)
}

// workflowListing is a project listing's workflows, parsed once so a round
// matching many client IDs does not decode the listing for each of them.
type workflowListing []listedWorkflow

type listedWorkflow struct {
	name, operation, seed string
}

// parseWorkflows reads the workflows of a project listing payload; an
// unreadable payload has none.
func parseWorkflows(payload string) workflowListing {
	var root []any
	if json.Unmarshal([]byte(payload), &root) != nil || len(root) < 2 {
		return nil
	}
	var out workflowListing
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
		name, _ := metadata[4].(string)
		workflow, _ := row[0].(string)
		out = append(out, listedWorkflow{name: name, operation: workflow, seed: seed})
	}
	return out
}

// forClient is mediaForClient over a parsed listing.
func (l workflowListing) forClient(clientID string) pendingVideo {
	if matches := l.forSeed(clientID); len(matches) > 0 {
		return matches[0]
	}
	return pendingVideo{}
}

// forSeed is mediaForSeed over a parsed listing.
func (l workflowListing) forSeed(seed string) []pendingVideo {
	if seed == "" {
		return nil
	}
	var matches []pendingVideo
	for _, w := range l {
		if strings.EqualFold(w.seed, seed) {
			matches = append(matches, pendingVideo{name: w.name, operation: w.operation, clientID: seed})
		}
	}
	return matches
}
