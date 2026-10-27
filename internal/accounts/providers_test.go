package accounts

import "testing"

type labeledSite struct{ *fakeSite }

func (labeledSite) Provider() Provider { return "acme" }
func (labeledSite) Label() string      { return "Acme Studio" }

func TestProviderEnumComesFromSites(t *testing.T) {
	enum := Providers(&fakeSite{}, labeledSite{&fakeSite{}})
	if len(enum) != 2 || enum[0].Value != ProviderMuse || enum[0].TSName != "Muse" {
		t.Fatalf("unlabelled site is named from its ID: %+v", enum)
	}
	if enum[1].Value != "acme" || enum[1].TSName != "AcmeStudio" || LabelOf(labeledSite{&fakeSite{}}) != "Acme Studio" {
		t.Fatalf("a site's label names it: %+v", enum)
	}
}
