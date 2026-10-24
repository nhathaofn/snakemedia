package flow

import (
	"encoding/json"
	"regexp"
)

// BuildBatchEnvelope constructs the standard Google Boq batchexecute outer envelope:
// [[[rpcID, JSON_STRING(inner), nil, "generic"]]]
func BuildBatchEnvelope(rpcID string, inner any) (string, error) {
	var innerJSON string
	switch v := inner.(type) {
	case string:
		innerJSON = v
	case []byte:
		innerJSON = string(v)
	default:
		b, err := json.Marshal(inner)
		if err != nil {
			return "", err
		}
		innerJSON = string(b)
	}

	outer := []any{
		[]any{
			[]any{
				rpcID,
				innerJSON,
				nil,
				"generic",
			},
		},
	}
	outerBytes, err := json.Marshal(outer)
	if err != nil {
		return "", err
	}
	return string(outerBytes), nil
}

// flowContext is the shared request context:
// [null, 22, null, null, null, projectID, null, null, null, null, [recaptchaToken, 1]]
func flowContext(projectID, recaptchaToken string) []any {
	return []any{
		nil,
		FlowSurfaceID,
		nil,
		nil,
		nil,
		projectID,
		nil,
		nil,
		nil,
		nil,
		[]any{recaptchaToken, 1},
	}
}

var uuidRegex = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func asList(v any) []any {
	list, _ := v.([]any)
	return list
}

func hasCapability(v any, want int) bool {
	switch x := v.(type) {
	case float64:
		return int(x) == want
	case []any:
		for _, item := range x {
			if hasCapability(item, want) {
				return true
			}
		}
	}
	return false
}

func firstOf(v any) any {
	if list := asList(v); len(list) > 0 {
		return list[0]
	}
	return nil
}

func intList(v any) []int {
	var out []int
	for _, item := range asList(v) {
		if n, ok := item.(float64); ok {
			out = append(out, int(n))
		}
	}
	return out
}
