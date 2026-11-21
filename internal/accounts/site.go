package accounts

import (
	"context"
	"sync"
	"time"

	"SnakeMedia/internal/browser"
)

type inspectScopeKey struct{}

// InspectScope is shared across every Site.Inspect poll of one Check or sign-in
// watch loop so a Site reloads a stale page at most once per check instead of
// on every poll.
type InspectScope struct {
	mu       sync.Mutex
	wentHome bool
	lastDiag string
}

// MarkHome reports whether this scope has not yet triggered a Home reload and
// marks it as having done so. Without a scope (a standalone Site.Inspect call),
// it returns true so one-off calls still recover once.
func (s *InspectScope) MarkHome() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wentHome {
		return false
	}
	s.wentHome = true
	return true
}

// FirstDiag reports whether diag has not yet been recorded for this scope,
// letting polling loops log non-ready diagnostics on state transitions only.
func (s *InspectScope) FirstDiag(diag string) bool {
	if s == nil || diag == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastDiag == diag {
		return false
	}
	s.lastDiag = diag
	return true
}

// WithInspectScope attaches a fresh InspectScope to ctx for one polling loop.
func WithInspectScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, inspectScopeKey{}, &InspectScope{})
}

// InspectScopeFrom returns the InspectScope carried by ctx, or nil for a
// standalone Site.Inspect call.
func InspectScopeFrom(ctx context.Context) *InspectScope {
	scope, _ := ctx.Value(inspectScopeKey{}).(*InspectScope)
	return scope
}

// Site is what a provider contributes to its accounts: the page their
// browsers keep open and how to read who is signed in there.
type Site interface {
	Provider() Provider
	// Page is the tab every account browser of this provider shows.
	Page() browser.Site
	// CookieDomain is given to imported cookies that name no domain;
	// AcceptsCookie keeps an import from planting cookies for other sites.
	CookieDomain() string
	AcceptsCookie(domain string) bool
	// Inspect reads the session in the account's browser. A page that is still
	// loading returns an error or SignedIn false; callers poll until a deadline.
	Inspect(ctx context.Context, b *browser.Browser, rec Record) (Inspection, error)
}

// Inspection is what a Site read from an account's page.
type Inspection struct {
	SignedIn bool
	// Identity names who is signed in (e.g. an email); it must not change for
	// an account once known.
	Identity string
	// Usage replaces the displayed allowance; nil keeps it.
	Usage []UsageItem
	// Data replaces the provider data; nil keeps it.
	Data any
	// Resume clears a rest the provider set earlier (its quota is back).
	Resume bool
	// Note is shown on a signed-in account (a remark, not an error).
	Note string
	// Fatal marks the account itself unusable (banned, dead, or unable to
	// generate): Check sets StatusError without waiting out PageReadySeconds.
	Fatal string
	// DisableAccount turns Enabled off when Fatal or PausedUntil is set.
	DisableAccount bool
	// PausedUntil rests the account until this time on a timed limit (e.g. 24h).
	PausedUntil time.Time
	AutoResume  bool
	// SignedOut tells Check's wait loop the site confirmed the session is
	// logged out (e.g. HTTP 401 or /login after Home retry), so waitSignedIn
	// stops waiting out PageReadySeconds immediately.
	SignedOut bool
	// VideoCheckDisabled marks SkipVideoCheck on the account while keeping it
	// SignedIn and Enabled so it keeps running image jobs.
	VideoCheckDisabled bool
	// VideoVerified confirms Check generated a video and clears SkipVideoCheck.
	VideoVerified bool
}

// VideoChecker is an optional extension of Site run only when the user
// explicitly triggers a video creation check (keeping regular Check fast).
type VideoChecker interface {
	CheckVideo(ctx context.Context, b *browser.Browser, rec Record, insp Inspection) (Inspection, error)
}

// SignInFollowUp is a Site with a step of its own once the user has signed in
// in the account's window (not on a plain check). The account shows as still
// signing in meanwhile and is signed in with the inspection it returns; the
// step reports its own failures (Inspection.Note) instead of failing the
// sign-in.
type SignInFollowUp interface {
	AfterSignIn(ctx context.Context, b *browser.Browser, rec Record, insp Inspection) Inspection
}
