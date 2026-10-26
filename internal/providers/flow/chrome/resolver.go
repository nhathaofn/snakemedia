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

const (
	MethodVideoStatus             = "/VideoFxService.BatchCheckAsyncVideoGenerationStatus"
	MethodModels                  = "/FlowService.GetModels"
	MethodProjects                = "/FlowService.GetProjects"
	MethodCreateProject           = "/AiSandbox.CreateProject"
	MethodProjectContents         = "/FlowService.GetProjectContents"
	MethodMedia                   = "/FlowService.GetMedia"
	MethodUploadImage             = "/FlowService.UploadImage"
	MethodGenerateImages          = "/FlowService.BatchGenerateImages"
	MethodUpsampleImage           = "/FlowService.UpsampleImage"
	MethodGenerateVideoText       = "/VideoFxService.BatchAsyncGenerateVideoText"
	MethodGenerateVideoReferences = "/VideoFxService.BatchAsyncGenerateVideoReferenceImages"
	MethodEditVideo               = "/VideoFxService.BatchAsyncGenerateVideoEditVideo"
)

type RPCRegistry struct {
	VideoStatus             string `json:"videoStatus,omitempty"`
	Models                  string `json:"models,omitempty"`
	Projects                string `json:"projects,omitempty"`
	CreateProject           string `json:"createProject,omitempty"`
	ProjectContents         string `json:"projectContents,omitempty"`
	Media                   string `json:"media,omitempty"`
	UploadImage             string `json:"uploadImage,omitempty"`
	GenerateImages          string `json:"generateImages,omitempty"`
	UpsampleImage           string `json:"upsampleImage,omitempty"`
	GenerateVideoText       string `json:"generateVideoText,omitempty"`
	GenerateVideoReferences string `json:"generateVideoReferences,omitempty"`
	EditVideo               string `json:"editVideo,omitempty"`
	Credits                 string `json:"credits"`
	Availability            string `json:"availability"`
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
	case "www.gstatic.com", "ssl.gstatic.com", FlowHost:
		return true
	}
	return false
}

func discoverRPCs(ctx context.Context, conn *websocket.Conn, build string, policy config.AccountConfig, required ...string) (RPCRegistry, error) {
	cacheKey := build + strings.Join(required, ",")
	if build == "" {
		return RPCRegistry{}, errors.New("missing runtime build label")
	}
	if cached, ok := registryCache.Load(cacheKey); ok {
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
		if (first.Hostname() == FlowHost) != (second.Hostname() == FlowHost) {
			return first.Hostname() != FlowHost
		}
		return sources[i] < sources[j]
	})
	registry, err := resolveRPCs(sources, policy.MaxDiscoveryScripts, policy.MaxDiscoveryBytes, func(source string) ([]byte, error) {
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
	}, required...)
	if len(required) == 0 || err == nil {
		registryCache.Store(cacheKey, registryEntry{Registry: registry, Err: err, Until: time.Now().Add(time.Duration(policy.RPCCacheSeconds) * time.Second)})
	}
	return registry, err
}

// Deduplication, an origin allowlist and a cumulative budget bound discovery.
func resolveRPCs(sources []string, maxScripts int, budget int64, load func(string) ([]byte, error), required ...string) (RPCRegistry, error) {
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
			field := registry.field(method)
			if field != nil {
				if *field != "" && *field != candidateID {
					return RPCRegistry{}, fmt.Errorf("ambiguous Flow RPC: %s", method)
				}
				*field = candidateID
			}
		}
		if (len(required) == 0 && registry.valid() && registry.Availability != "") || (len(required) > 0 && registry.Has(required...)) {
			return registry, nil
		}
	}
	if (len(required) == 0 && registry.valid()) || (len(required) > 0 && registry.Has(required...)) {
		return registry, nil
	}
	missing := []string{}
	for _, method := range required {
		if !registry.Has(method) {
			missing = append(missing, method)
		}
	}
	return registry, fmt.Errorf("Flow chưa tải đủ RPC của build hiện tại (scripts=%d, missing=%s). Mở project Flow rồi thử lại.", len(seen), strings.Join(missing, ","))
}

func (r RPCRegistry) ids() string {
	if r.Availability == "" {
		return r.Credits
	}
	return strings.Join([]string{r.Credits, r.Availability}, ",")
}

func (r *RPCRegistry) field(method string) *string {
	switch method {
	case MethodVideoStatus:
		return &r.VideoStatus
	case creditsMethod:
		return &r.Credits
	case availabilityMethod:
		return &r.Availability
	case MethodModels:
		return &r.Models
	case MethodProjects:
		return &r.Projects
	case MethodCreateProject:
		return &r.CreateProject
	case MethodProjectContents:
		return &r.ProjectContents
	case MethodMedia:
		return &r.Media
	case MethodUploadImage:
		return &r.UploadImage
	case MethodGenerateImages:
		return &r.GenerateImages
	case MethodUpsampleImage:
		return &r.UpsampleImage
	case MethodGenerateVideoText:
		return &r.GenerateVideoText
	case MethodGenerateVideoReferences:
		return &r.GenerateVideoReferences
	case MethodEditVideo:
		return &r.EditVideo
	}
	return nil
}

func (r RPCRegistry) ID(method string) string {
	p := r.field(method)
	if p == nil {
		return ""
	}
	return *p
}
func (r RPCRegistry) Has(methods ...string) bool {
	for _, m := range methods {
		if !rpcIDPattern.MatchString(r.ID(m)) {
			return false
		}
	}
	return true
}

// DiscoverFlowRPCs reads descriptors from the account's currently loaded scripts.
// Missing methods fail explicitly; a captured RPC ID is never used as fallback.
func DiscoverFlowRPCs(ctx context.Context, port int, build string, policy config.AccountConfig, required ...string) (RPCRegistry, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	var pages []pageEntry
	if err := localJSON(ctx, port, "/json", policy, &pages); err != nil {
		return RPCRegistry{}, err
	}
	for _, page := range pages {
		if _, ok := page.flowPage(); !ok {
			continue
		}
		if !localWebSocket(page.WebSocketDebuggerURL, port) {
			return RPCRegistry{}, errors.New("invalid account page endpoint")
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, page.WebSocketDebuggerURL, nil)
		if err != nil {
			return RPCRegistry{}, err
		}
		defer conn.Close()
		conn.SetReadLimit(policy.MaxDiscoveryBytes)
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		return discoverRPCs(ctx, conn, build, policy, required...)
	}
	return RPCRegistry{}, ErrPageNotReady
}
