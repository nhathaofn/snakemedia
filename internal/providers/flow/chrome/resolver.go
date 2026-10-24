package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

// Stable method names are the contract; obfuscated IDs come from active scripts.
const creditsMethod = "/VideoFxService.GetCredits"
const availabilityMethod = "/VideoFxService.CheckFlowAvailability"

type RPCRegistry struct {
	Credits      string `json:"credits"`
	Availability string `json:"availability"`
}

var rpcIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,30}$`)
var rpcChunkPattern = regexp.MustCompile(`\{[^{}]*`)
var rpcKVPattern = regexp.MustCompile(`(?:[A-Za-z0-9_$]+|["'][^"']+["'])\s*:\s*["']([^"']+)["']`)
var registryCache sync.Map

type registryEntry struct {
	Registry RPCRegistry
	Err      error
	Until    time.Time
}

func (r RPCRegistry) valid() bool {
	return rpcIDPattern.MatchString(r.Credits) && (r.Availability == "" || (rpcIDPattern.MatchString(r.Availability) && r.Credits != r.Availability))
}

func scriptOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch u.Hostname() {
	case "www.gstatic.com", "ssl.gstatic.com", "flow.google.com":
		return true
	}
	return false
}

func discoverRPCs(ctx context.Context, conn *websocket.Conn, build string, policy config.AccountConfig) (RPCRegistry, error) {
	if build == "" {
		return RPCRegistry{}, errors.New("missing runtime build label")
	}
	if cached, ok := registryCache.Load(build); ok {
		entry := cached.(registryEntry)
		if entry.Err == nil || time.Now().Before(entry.Until) {
			return entry.Registry, entry.Err
		}
	}
	// Read the scripts Chrome has actually compiled, including lazily loaded
	// bundles. Re-downloading document.scripts from Go missed runtime sources and
	// could fail independently of the user's already authenticated browser.
	scripts := make(map[string]string)
	handle := func(method string, params json.RawMessage) {
		if method != "Debugger.scriptParsed" {
			return
		}
		var script struct {
			ID  string `json:"scriptId"`
			URL string `json:"url"`
		}
		if json.Unmarshal(params, &script) != nil || !scriptOrigin(script.URL) || script.ID == "" {
			return
		}
		if len(scripts) < policy.MaxDiscoveryScripts*2 || scripts[script.URL] != "" {
			scripts[script.URL] = script.ID
		}
	}
	nextID := 4
	call := func(method string, params, out any) error {
		id := nextID
		nextID++
		return cdpCall(ctx, conn, id, method, params, out, handle)
	}
	if err := call("Debugger.enable", map[string]any{}, nil); err != nil {
		return RPCRegistry{}, err
	}
	defer func() { _ = call("Debugger.disable", map[string]any{}, nil) }()
	if err := call("Debugger.setSkipAllPauses", map[string]any{"skip": true}, nil); err != nil {
		return RPCRegistry{}, err
	}
	if err := call("Runtime.evaluate", map[string]any{"expression": "0", "returnByValue": true}, nil); err != nil {
		return RPCRegistry{}, err
	}

	// Also discover script tags present in the DOM in case any were loaded before or missed by events.
	var domScripts []string
	if err := evaluate(ctx, conn, nextID, `JSON.stringify(Array.from(document.querySelectorAll('script[src]')).map(s=>s.src).filter(Boolean))`, &domScripts); err == nil {
		nextID++
		for _, urlStr := range domScripts {
			if scriptOrigin(urlStr) && scripts[urlStr] == "" {
				scripts[urlStr] = ""
			}
		}
	}

	sources := make([]string, 0, len(scripts))
	for source := range scripts {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		first, _ := url.Parse(sources[i])
		second, _ := url.Parse(sources[j])
		if (first.Hostname() == "flow.google.com") != (second.Hostname() == "flow.google.com") {
			return first.Hostname() != "flow.google.com"
		}
		return sources[i] < sources[j]
	})
	registry, err := resolveRPCs(sources, policy.MaxDiscoveryScripts, policy.MaxResponseBytes, func(source string) ([]byte, error) {
		if scriptID := scripts[source]; scriptID != "" {
			var result struct {
				Source string `json:"scriptSource"`
			}
			if err := call("Debugger.getScriptSource", map[string]any{"scriptId": scriptID}, &result); err == nil && len(result.Source) > 0 {
				return []byte(result.Source), nil
			}
		}
		// Fallback: evaluate fetch from browser page (serves directly from browser cache)
		var text string
		fetchExpr := fmt.Sprintf("(async()=>{const r=await fetch(%q);return await r.text();})()", source)
		id := nextID
		nextID++
		if err := evaluate(ctx, conn, id, fetchExpr, &text); err == nil && len(text) > 0 {
			return []byte(text), nil
		}
		return nil, errors.New("cannot read script source")
	})
	registryCache.Store(build, registryEntry{Registry: registry, Err: err, Until: time.Now().Add(time.Duration(policy.PollMaxIntervalMS) * time.Millisecond)})
	return registry, err
}

// Deduplication, an origin allowlist and a cumulative budget bound discovery.
func resolveRPCs(sources []string, maxScripts int, budget int64, load func(string) ([]byte, error)) (RPCRegistry, error) {
	var registry RPCRegistry
	seen := make(map[string]bool)
	for _, source := range sources {
		if seen[source] || !scriptOrigin(source) {
			continue
		}
		seen[source] = true
		if len(seen) > maxScripts {
			return RPCRegistry{}, errors.New("Flow RPC discovery reached the configured script limit")
		}
		data, err := load(source)
		if err != nil {
			continue
		}
		budget -= int64(len(data))
		if budget < 0 {
			return RPCRegistry{}, errors.New("Flow RPC discovery exceeded the configured response budget")
		}
		for _, chunk := range rpcChunkPattern.FindAll(data, -1) {
			pairs := rpcKVPattern.FindAllSubmatch(chunk, -1)
			if len(pairs) < 2 {
				continue
			}
			var method string
			var candidateID string
			candidateCount := 0
			for _, pair := range pairs {
				val := string(pair[1])
				if len(val) > 0 && val[0] == '/' {
					method = val
				} else if rpcIDPattern.MatchString(val) {
					candidateID = val
					candidateCount++
				}
			}
			if method == "" || candidateCount != 1 {
				continue
			}
			switch method {
			case creditsMethod:
				if registry.Credits != "" && registry.Credits != candidateID {
					return RPCRegistry{}, errors.New("ambiguous credits RPC")
				}
				registry.Credits = candidateID
			case availabilityMethod:
				if registry.Availability != "" && registry.Availability != candidateID {
					return RPCRegistry{}, errors.New("ambiguous availability RPC")
				}
				registry.Availability = candidateID
			}
		}
		if registry.valid() && registry.Availability != "" {
			return registry, nil
		}
	}
	if registry.valid() {
		return registry, nil
	}
	return RPCRegistry{}, errors.New("Không tìm thấy RPC tài khoản trong bundle Flow hiện tại. Hãy tải lại trang; không sử dụng mã RPC của build cũ.")
}

func (r RPCRegistry) ids() string {
	if r.Availability == "" {
		return r.Credits
	}
	return strings.Join([]string{r.Credits, r.Availability}, ",")
}
