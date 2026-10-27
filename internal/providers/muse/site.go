package muse

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
)

// museData is what a Muse account keeps beside the shared fields.
type museData struct {
	Quota        *MuseQuota        `json:"quota,omitempty"`
	Capabilities *MuseCapabilities `json:"capabilities,omitempty"`
	LastSentAt   string            `json:"lastSentAt,omitempty"`
	// RestReason is why the account rests (restQuota or restLimit); empty
	// for a rest set before the reason was kept, treated as restQuota.
	RestReason string `json:"restReason,omitempty"`
}

// MuseAccount is what the queue knows about one account.
type MuseAccount struct {
	ID          string
	Name        string
	Enabled     bool
	Status      accounts.Status
	PausedUntil string
	Quota       *MuseQuota
	LastSentAt  string
	// NoRoute is why the account has no proxy route now; such an account
	// takes no work until it has one.
	NoRoute string
}

func museView(rec accounts.Record) MuseAccount {
	var data museData
	_ = accounts.DecodeData(rec, &data)
	return MuseAccount{
		ID: rec.ID, Name: rec.Label(), Enabled: rec.Enabled, Status: rec.Status,
		PausedUntil: rec.PausedUntil, Quota: data.Quota, LastSentAt: data.LastSentAt,
	}
}

// Site is muse.ai for the accounts service.
type Site struct {
	cfg *config.Manager
}

// NewSite reads Muse sessions with the shared account policy.
func NewSite(cfg *config.Manager) *Site { return &Site{cfg: cfg} }

func (*Site) Provider() accounts.Provider { return accounts.ProviderMuse }

func (*Site) Page() browser.Site { return musePage }

// musePage keeps Muse (a Next.js app) rendering in a hidden or background tab.
var musePage = browser.Site{Home: MuseURL, Owns: museURL, Boot: visibleShim}

func museURL(u *url.URL) bool {
	return u.Scheme == "https" && (u.Host == "muse.ai" || u.Host == "www.muse.ai")
}

func (*Site) CookieDomain() string { return ".muse.ai" }

// AcceptsCookie keeps only muse.ai cookies.
func (*Site) AcceptsCookie(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	return d == "muse.ai" || strings.HasSuffix(d, ".muse.ai")
}

// Inspect asks the page whether it is signed in, then reads the subscription
// and what the account may send. A value that cannot be read keeps the last
// one read. A quota with room left ends a rest set because the weekly quota
// ran out; a rest for another limit Muse named runs its time.
func (s *Site) Inspect(ctx context.Context, b *browser.Browser, rec accounts.Record) (accounts.Inspection, error) {
	cfg := s.cfg.Get()
	page := &chromePage{
		port: b.Port, policy: cfg.Accounts,
		callTimeout: time.Duration(cfg.Accounts.RequestTimeoutSeconds) * time.Second,
		sendTimeout: time.Duration(cfg.Muse.SendTimeoutSeconds) * time.Second,
	}
	state, err := page.Login(ctx)
	if err == nil && state.HTTP == 200 && !state.Chat {
		// Signed in but on a page without the chat (settings, a shared
		// link): go home and ask again rather than report signed out.
		if page.Home(ctx) == nil {
			state, err = page.Login(ctx)
		}
	}
	if err != nil || !state.LoggedIn {
		return accounts.Inspection{}, err
	}
	var data museData
	if err := accounts.DecodeData(rec, &data); err != nil {
		return accounts.Inspection{}, err
	}
	quota, _ := page.Quota(ctx)
	if quota != nil {
		data.Quota = quota
	}
	if caps, _ := page.Capabilities(ctx); caps != nil {
		data.Capabilities = caps
	}
	resume := quotaResumes(data, quota)
	if resume {
		data.RestReason = ""
	}
	return accounts.Inspection{SignedIn: true, Usage: quotaUsage(data.Quota), Data: data, Resume: resume}, nil
}

// quotaResumes reports whether a freshly read quota ends the account's rest:
// only a rest for the weekly quota, and only once that quota has room.
func quotaResumes(data museData, quota *MuseQuota) bool {
	return quota != nil && !quota.weeklyOut() && data.RestReason != restLimit
}

// quotaUsage shows the quota on the account card in Muse's own words.
func quotaUsage(q *MuseQuota) []accounts.UsageItem {
	if q == nil {
		return []accounts.UsageItem{{Label: "Hạn mức", Value: "Chưa đọc được"}}
	}
	or := func(s, fallback string) string {
		if s != "" {
			return s
		}
		return fallback
	}
	items := []accounts.UsageItem{{
		Label: or(q.UsageLabel, or(q.Tier, "Hạn mức tuần")),
		Value: or(q.UsageValue, fmt.Sprintf("%g%%", q.PercentUsed)),
	}}
	if reset := q.resetsAt(); !reset.IsZero() {
		items = append(items, accounts.UsageItem{Label: "Đặt lại", Value: reset.Local().Format("15:04 02/01")})
	}
	if q.TopupTotal > 0 || q.TopupValue != "" {
		items = append(items, accounts.UsageItem{
			Label: or(q.TopupLabel, "Token nạp thêm"),
			Value: or(q.TopupValue, fmt.Sprintf("%g/%g", q.TopupBalance, q.TopupTotal)),
		})
	}
	if q.Tier != "" && q.UsageLabel != "" {
		items = append(items, accounts.UsageItem{Label: "Gói", Value: q.Tier})
	}
	return items
}
