package chrome

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

func TestResolveActiveBuildRPCsAndLimits(t *testing.T) {
	first := "https://www.gstatic.com/first.js"
	second := "https://www.gstatic.com/second.js"
	// Tests arbitrary/minified property names, quoted/unquoted keys, and ordering variations across builds.
	sources := map[string]string{first: `var a={customMethod:'/VideoFxService.GetCredits',Za:true,'customID':'newCreditID'};`, second: `var b={liveIdProp:"newAvailabilityID",liveMethodProp:"/VideoFxService.CheckFlowAvailability",Za:!0};`}
	loads := 0
	load := func(source string) ([]byte, error) { loads++; return []byte(sources[source]), nil }
	registry, err := resolveRPCs([]string{"https://attacker.invalid/x", first, first, second}, 2, 1024, load)
	if err != nil || registry.Credits != "newCreditID" || registry.Availability != "newAvailabilityID" || loads != 2 {
		t.Fatalf("dynamic RPC lookup failed: %+v %v", registry, err)
	}
	if _, err := resolveRPCs([]string{first, second}, 1, 1024, load); err == nil {
		t.Fatal("script cap ignored")
	}
	if _, err := resolveRPCs([]string{first, second}, 2, 1, load); err == nil {
		t.Fatal("byte cap ignored")
	}
	for _, source := range []string{"http://www.gstatic.com/x", "https://www.gstatic.com.attacker.invalid/x", "https://user:pass@www.gstatic.com/x", "https://www.gstatic.com:1234/x"} {
		if scriptOrigin(source) {
			t.Fatalf("unsafe script origin %s", source)
		}
	}
}

func TestResolveMatchesLiveBuildDescriptors(t *testing.T) {
	// Verifies exact descriptor snippets found in live Google Flow builds (e.g. boq_labs-ai-sandbox-frontend_20261021)
	script := "https://www.gstatic.com/live_flow_bundle.js"
	content := `var Eza=_.Ed(_.Gz);var Fza={Xa:"nzlxg",Za:"/VideoFxService.GetCredits",kb:!0,Ya:_.Gz,lb:Dza};Fza[_.Iy]=!0;var xza={Xa:"rThb8d",Za:"/VideoFxService.CheckFlowAvailability",kb:!0,Ya:class extends _.n{constructor(a){super(a)}}};`
	registry, err := resolveRPCs([]string{script}, 5, 10240, func(string) ([]byte, error) { return []byte(content), nil })
	if err != nil {
		t.Fatalf("failed to resolve live build descriptors: %v", err)
	}
	if registry.Credits != "nzlxg" || registry.Availability != "rThb8d" {
		t.Fatalf("unexpected RPC IDs: %+v", registry)
	}
}

func TestResolverMatchesReferenceBundleWhenPresent(t *testing.T) {
	path := filepath.Join("..", "..", "..", "references", "flow-research", "observations", "bundles", "bundle_0.js")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("optional reference bundle not present")
	}
	p := config.DefaultFlowPagePolicy()
	registry, err := resolveRPCs([]string{"https://www.gstatic.com/reference.js"}, p.MaxDiscoveryScripts, p.MaxResponseBytes, func(string) ([]byte, error) { return os.ReadFile(path) })
	if err != nil || !registry.valid() {
		t.Fatalf("resolver does not match documented bundle format: %v", err)
	}
}

func TestStudioMethodsResolveFromRealReferenceBundles(t *testing.T) {
	root := filepath.Join("..", "..", "..", "references", "flow-research", "observations", "bundles")
	names := []string{"bundle_0.js", "chunk_studio_1.js"}
	sources := []string{}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Skip("reference bundles not present")
		}
		sources = append(sources, "https://www.gstatic.com/"+name)
	}
	registry, err := resolveRPCs(sources, config.DefaultFlowPagePolicy().MaxDiscoveryScripts, config.DefaultFlowPagePolicy().MaxDiscoveryBytes, func(source string) ([]byte, error) { return os.ReadFile(filepath.Join(root, filepath.Base(source))) }, MethodModels, MethodProjects, MethodCreateProject, MethodEditVideo, MethodVideoStatus, MethodProjectContents, MethodMedia, MethodUploadImage, MethodUpsampleImage, MethodGenerateVideoText, MethodGenerateVideoReferences)
	if err != nil || !registry.Has(MethodModels, MethodProjects, MethodCreateProject, MethodEditVideo, MethodVideoStatus, MethodProjectContents, MethodMedia, MethodUploadImage, MethodUpsampleImage, MethodGenerateVideoText, MethodGenerateVideoReferences) {
		t.Fatalf("real bundle studio descriptors: %v", err)
	}
}

func TestResolveImageGenerationDescriptorFromProjectBundle(t *testing.T) {
	// Snippet shape captured from the 2026-10-25 project-page bundle.
	script := "https://www.gstatic.com/project_bundle.js"
	content := `y6a=_.Gd(x6a),z6a={Wa:"ogiZ0b",hb:"/FlowService.BatchGenerateImages",kb:!1,Ya:class extends _.m{}};O8a={Wa:"maseQ",hb:"/FlowService.UploadImage",kb:!1,Ya:class extends _.m{}};`
	registry, err := resolveRPCs([]string{script}, 5, 10240, func(string) ([]byte, error) { return []byte(content), nil }, MethodGenerateImages, MethodUploadImage)
	if err != nil || registry.ID(MethodGenerateImages) != "ogiZ0b" || registry.ID(MethodUploadImage) != "maseQ" {
		t.Fatalf("image RPCs not resolved: %+v %v", registry, err)
	}
}

func TestResolveNamelessDescriptorsFromFallbackIDs(t *testing.T) {
	// Snippet shape captured from the boq_labs-ai-sandbox-frontend_20261028 home bundle:
	// descriptors carry only the ID, no method name.
	content := `var rYa=_.Hd(_.XM),xYa={Wa:"UpteDb",hb:void 0,Za:!0,Xa:class extends _.m{constructor(a){super(a)}},kb:_.xA};xYa[_.Nz]=!0;yYa={Wa:"jHPbke",hb:void 0,Za:!1,Xa:_.RM,kb:_.QA};`
	load := func(string) ([]byte, error) { return []byte(content), nil }
	registry, err := resolveRPCs([]string{"https://www.gstatic.com/home.js"}, 5, 10240, load, MethodProjects, MethodCreateProject)
	if err != nil || registry.ID(MethodProjects) != "UpteDb" || registry.ID(MethodCreateProject) != "jHPbke" {
		t.Fatalf("nameless descriptors not resolved: %+v %v", registry, err)
	}
	// An ID the loaded scripts do not declare is never assumed.
	if _, err := resolveRPCs([]string{"https://www.gstatic.com/home.js"}, 5, 10240, load, MethodModels); err == nil {
		t.Fatal("GetModels resolved without its descriptor")
	}
	// A named descriptor wins over the fallback ID.
	named := content + `z={Wa:"newID",hb:"/FlowService.GetProjects",kb:!1};`
	registry, err = resolveRPCs([]string{"https://www.gstatic.com/a.js"}, 5, 10240, func(string) ([]byte, error) { return []byte(named), nil }, MethodProjects)
	if err != nil || registry.ID(MethodProjects) != "newID" {
		t.Fatalf("named descriptor did not win: %+v %v", registry, err)
	}
}

func TestRPCRegistryMergePreservesExistingFields(t *testing.T) {
	stored := RPCRegistry{
		Models:         "HTrJv",
		UploadImage:    "maseQ",
		GenerateImages: "ogiZ0b",
		UpsampleImage:  "SPrCad",
		EditVideo:      "jIps6",
		Credits:        "oldCred",
	}
	partial := RPCRegistry{
		Credits:      "newCred",
		Availability: "rThb8d",
	}
	merged := partial.Merge(stored)
	if merged.Credits != "newCred" || merged.Availability != "rThb8d" {
		t.Fatalf("fresh values did not win: %+v", merged)
	}
	if merged.Models != "HTrJv" || merged.UploadImage != "maseQ" || merged.GenerateImages != "ogiZ0b" || merged.UpsampleImage != "SPrCad" || merged.EditVideo != "jIps6" {
		t.Fatalf("stored studio RPCs were clobbered: %+v", merged)
	}
}

func TestDiscoverRPCsDOMScriptFetchFallbackDecodesJSONStringifiedJS(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	rawJS := `var a={id:"nzlxg",m:"/VideoFxService.GetCredits"};var b={id:"rThb8d",m:"/VideoFxService.CheckFlowAvailability"};var c={id:"maseQ",m:"/FlowService.UploadImage"};`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(msg, &req); err != nil {
				return
			}
			var reply any
			switch req.Method {
			case "Debugger.enable", "Debugger.disable", "Debugger.setSkipAllPauses":
				reply = map[string]any{"id": req.ID, "result": map[string]any{}}
			case "Runtime.evaluate":
				expr, _ := req.Params["expression"].(string)
				switch {
				case strings.Contains(expr, "querySelectorAll"):
					val, _ := json.Marshal([]string{"https://www.gstatic.com/dom_fallback.js"})
					reply = map[string]any{"id": req.ID, "result": map[string]any{"result": map[string]any{"value": string(val)}}}
				case strings.Contains(expr, "fetch("):
					if !strings.Contains(expr, "JSON.stringify(") {
						// Reproduce browser.Evaluate failure when raw JS is returned unquoted.
						reply = map[string]any{"id": req.ID, "result": map[string]any{"result": map[string]any{"value": rawJS}}}
					} else {
						val, _ := json.Marshal(rawJS)
						reply = map[string]any{"id": req.ID, "result": map[string]any{"result": map[string]any{"value": string(val)}}}
					}
				default:
					reply = map[string]any{"id": req.ID, "result": map[string]any{"result": map[string]any{"value": 0}}}
				}
			}
			_ = conn.WriteJSON(reply)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	policy := config.DefaultFlowPagePolicy()
	policy.RPCCacheSeconds = 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reg, err := discoverRPCs(ctx, conn, "build-dom-fallback-test", policy, MethodUploadImage)
	if err != nil {
		t.Fatalf("DOM script fetch fallback failed: %v", err)
	}
	if reg.ID(MethodUploadImage) != "maseQ" || reg.Credits != "nzlxg" {
		t.Fatalf("unexpected registry from DOM fallback: %+v", reg)
	}
}
