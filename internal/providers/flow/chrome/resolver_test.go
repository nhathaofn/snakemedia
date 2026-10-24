package chrome

import (
	"os"
	"path/filepath"
	"testing"

	"SnakeMedia/internal/config"
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
	p := config.DefaultAccountConfig()
	registry, err := resolveRPCs([]string{"https://www.gstatic.com/reference.js"}, p.MaxDiscoveryScripts, p.MaxResponseBytes, func(string) ([]byte, error) { return os.ReadFile(path) })
	if err != nil || !registry.valid() {
		t.Fatalf("resolver does not match documented bundle format: %v", err)
	}
}
