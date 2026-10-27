package flow

import (
	"encoding/json"
	"strings"
)

// walkJSPB visits every value of a decoded batchexecute payload depth first:
// a list or object before its children, and every other string trimmed.
// Strings holding nested JSON (Flow wraps whole replies in them) are decoded
// and walked in their place. visit returns false to stop the walk; walkJSPB
// reports whether it ran to the end.
func walkJSPB(v any, visit func(any) bool) bool {
	switch x := v.(type) {
	case []any:
		if !visit(x) {
			return false
		}
		for _, child := range x {
			if !walkJSPB(child, visit) {
				return false
			}
		}
	case map[string]any:
		if !visit(x) {
			return false
		}
		for _, child := range x {
			if !walkJSPB(child, visit) {
				return false
			}
		}
	case string:
		trimmed := strings.TrimSpace(x)
		if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
			var nested any
			if json.Unmarshal([]byte(trimmed), &nested) == nil {
				return walkJSPB(nested, visit)
			}
		}
		return visit(trimmed)
	}
	return true
}

// anyJSPB reports whether match holds for some value walkJSPB visits.
func anyJSPB(v any, match func(any) bool) bool {
	return !walkJSPB(v, func(x any) bool { return !match(x) })
}

// jspbStrings returns every (trimmed) string walkJSPB visits, in order.
func jspbStrings(v any) []string {
	var out []string
	walkJSPB(v, func(x any) bool {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
		return true
	})
	return out
}

// normalizedKey folds an object key for loose matching ("generated_image" →
// "generatedimage").
func normalizedKey(key string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
}
