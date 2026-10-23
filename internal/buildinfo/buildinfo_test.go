package buildinfo

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	info, err := Parse([]byte(`{"name":"Demo","info":{"companyName":"Acme","productName":"Demo App","productVersion":"2.3.4"}}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if info.Name != "Demo" || info.ProductName != "Demo App" || info.Version != "2.3.4" {
		t.Errorf("unexpected info: %+v", info)
	}
	if got, want := info.SingleInstanceID(), "app.acme.demo"; got != want {
		t.Errorf("SingleInstanceID() = %q; want %q", got, want)
	}
}

func TestParse_ProductNameFallsBackToName(t *testing.T) {
	info, err := Parse([]byte(`{"name":"Demo","info":{"productVersion":"1.0.0"}}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if info.ProductName != "Demo" {
		t.Errorf("ProductName = %q; want Demo", info.ProductName)
	}
}

func TestParse_Invalid(t *testing.T) {
	cases := map[string]string{
		"malformed":       `{`,
		"missing name":    `{"info":{"productVersion":"1.0.0"}}`,
		"missing version": `{"name":"Demo"}`,
	}
	for name, manifest := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(manifest)); !errors.Is(err, ErrInvalidManifest) {
				t.Errorf("expected ErrInvalidManifest, got %v", err)
			}
		})
	}
}
