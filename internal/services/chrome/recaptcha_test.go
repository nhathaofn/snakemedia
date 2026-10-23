package chrome

import (
	"strings"
	"testing"
)

func TestBuildRecaptchaMintScript(t *testing.T) {
	script := BuildRecaptchaMintScript("TEST_ACTION")
	if !strings.Contains(script, "TEST_ACTION") {
		t.Errorf("expected script to contain TEST_ACTION")
	}
	if !strings.Contains(script, FlowRecaptchaSiteKey) {
		t.Errorf("expected script to contain default site key %s", FlowRecaptchaSiteKey)
	}
	if !strings.Contains(script, "extension_hijack_detected") {
		t.Errorf("expected script to contain anti-tamper trap check")
	}
	if !strings.Contains(script, "Object.assign = realObjectAssign") {
		t.Errorf("expected script to restore Object.assign in finally block")
	}
}
