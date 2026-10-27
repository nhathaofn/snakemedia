package accounts

import (
	"context"

	"SnakeMedia/internal/browser"
)

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
}

// SignInFollowUp is a Site with a step of its own once the user has signed in
// in the account's window (not on a plain check). The account shows as still
// signing in meanwhile and is signed in with the inspection it returns; the
// step reports its own failures (Inspection.Note) instead of failing the
// sign-in.
type SignInFollowUp interface {
	AfterSignIn(ctx context.Context, b *browser.Browser, rec Record, insp Inspection) Inspection
}
