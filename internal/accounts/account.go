// Package accounts owns every provider's accounts in one place: one Chrome
// profile each, sign-in through a window or imported cookies, the shared proxy
// list, and the encrypted store. A provider plugs in as a Site that knows its
// page and how to read who is signed in; its own runtime data rides along in
// Record.Data.
package accounts

import (
	"encoding/json"
	"strings"

	"SnakeMedia/internal/browser"
)

// Provider names the external service an account belongs to.
type Provider string

const (
	ProviderFlow Provider = "flow"
	ProviderMuse Provider = "muse"
)

// ProviderEnum is bound to the frontend as the TypeScript enum
// accounts.Provider, built from the Sites the app registers, so a new
// provider needs no list here.
type ProviderEnum = []struct {
	Value  Provider
	TSName string
}

// Labeled is a Site that names its provider for people ("Flow"); without it
// the provider ID is capitalised.
type Labeled interface {
	Label() string
}

// LabelOf names a site's provider for people.
func LabelOf(site Site) string {
	if l, ok := site.(Labeled); ok && l.Label() != "" {
		return l.Label()
	}
	id := string(site.Provider())
	if id == "" {
		return id
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

// Providers lists the registered sites' providers for the enum binding.
func Providers(sites ...Site) ProviderEnum {
	out := make(ProviderEnum, 0, len(sites))
	for _, site := range sites {
		out = append(out, struct {
			Value  Provider
			TSName string
		}{site.Provider(), strings.ReplaceAll(LabelOf(site), " ", "")})
	}
	return out
}

// Status is what the app last learned about an account's session.
type Status string

const (
	// StatusUnverified: saved but not checked yet (new, or cookies just imported).
	StatusUnverified Status = "unverified"
	// StatusSigningIn: a sign-in window is open and waiting for the user.
	StatusSigningIn Status = "signing_in"
	StatusSignedIn  Status = "signed_in"
	StatusSignedOut Status = "signed_out"
	StatusError     Status = "error"
)

// AllStatuses is bound to the frontend as a TypeScript enum (accounts.Status).
var AllStatuses = []struct {
	Value  Status
	TSName string
}{
	{StatusUnverified, "Unverified"}, {StatusSigningIn, "SigningIn"}, {StatusSignedIn, "SignedIn"},
	{StatusSignedOut, "SignedOut"}, {StatusError, "Error"},
}

// Source is how the account's session was obtained.
type Source string

const (
	SourceBrowser Source = "browser"
	SourceCookies Source = "cookies"
)

// AllSources is bound to the frontend as a TypeScript enum (accounts.Source).
var AllSources = []struct {
	Value  Source
	TSName string
}{{SourceBrowser, "Browser"}, {SourceCookies, "Cookies"}}

// UsageItem is one line of what the provider reports about an account's
// allowance (credits, quota, plan), already worded for display.
type UsageItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Account is the public view of an account; it never carries cookies,
// session tokens or the line of a proxy picked automatically from the list
// (ProxyAuto stays in Record; ProxyInUse is masked).
type Account struct {
	ID       string   `json:"id"`
	Provider Provider `json:"provider"`
	// Name is the user's label; Identity is who the site says is signed in.
	Name     string      `json:"name"`
	Identity string      `json:"identity,omitempty"`
	Source   Source      `json:"source"`
	Status   Status      `json:"status"`
	Enabled  bool        `json:"enabled"`
	Usage    []UsageItem `json:"usage"`
	// Proxy is the assignment: "" takes one from the shared list, "none" goes
	// direct, anything else is the account's own proxy line. That line is
	// sent as the user entered it so the proxy dialog can edit it; the user
	// can read the whole list on the Proxy page anyway.
	Proxy string `json:"proxy"`
	// ProxyInUse names the proxy the account runs on, without secrets;
	// ProxyRotating says whether that proxy can be asked for a new IP.
	ProxyInUse    string `json:"proxyInUse,omitempty"`
	ProxyRotating bool   `json:"proxyRotating,omitempty"`
	ProxyStatus   string `json:"proxyStatus,omitempty"`
	ProxyIP       string `json:"proxyIp,omitempty"`
	// PausedUntil rests the account after the site said it hit a limit (RFC 3339).
	PausedUntil string `json:"pausedUntil,omitempty"`
	Message     string `json:"message,omitempty"`
	// NoRoute is why the account cannot get a proxy route now. It is worked
	// out for each listing, never saved; an account with one takes no work.
	NoRoute       string `json:"noRoute,omitempty"`
	LastCheckedAt string `json:"lastCheckedAt,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// Record is an account as stored, with what must stay in Go.
type Record struct {
	Account
	ProfileDir string `json:"profileDir"`
	// ProxyAuto is the list line an automatic pick pinned to the account.
	ProxyAuto string           `json:"proxyAuto,omitempty"`
	Cookies   []browser.Cookie `json:"cookies,omitempty"`
	// CookieVersion changes on every import, so a profile is seeded once.
	CookieVersion string `json:"cookieVersion,omitempty"`
	// Data is the provider's own runtime data (session values, quota).
	Data json.RawMessage `json:"data,omitempty"`
}

// DecodeData reads the provider data of rec into out; empty data leaves out as is.
func DecodeData(rec Record, out any) error {
	if len(rec.Data) == 0 {
		return nil
	}
	return json.Unmarshal(rec.Data, out)
}

// SetData replaces the provider data of rec.
func (r *Record) SetData(data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	r.Data = encoded
	return nil
}

// Ready reports whether the account may take work: enabled and signed in.
func (a Account) Ready() bool { return a.Enabled && a.Status == StatusSignedIn }

// Label is how messages name the account.
func (a Account) Label() string {
	switch {
	case a.Name != "":
		return a.Name
	case a.Identity != "":
		return a.Identity
	default:
		return "Tài khoản mới"
	}
}
