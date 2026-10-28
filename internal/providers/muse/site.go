package muse

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
)

// museData is what a Muse account keeps beside the shared fields.
type museData struct {
	Quota        *MuseQuota        `json:"quota,omitempty"`
	Capabilities *MuseCapabilities `json:"capabilities,omitempty"`
	LastSentAt   string            `json:"lastSentAt,omitempty"`
	// RestReason is why the account rests (restQuota or restLimit); empty
	// for a rest set before the reason was kept, treated as restQuota.
	RestReason string `json:"restReason,omitempty"`
	// SetupSentAt is when Muse took the setup message (config
	// Muse.SetupMessage), sent once per account after its first sign-in;
	// SetupDoneAt is when Muse replied, and SetupReply that reply, trimmed.
	SetupSentAt string `json:"setupSentAt,omitempty"`
	SetupDoneAt string `json:"setupDoneAt,omitempty"`
	SetupReply  string `json:"setupReply,omitempty"`
}

// setupSent reports whether Muse has taken the account's setup message, so it
// is never sent again.
func (d museData) setupSent() bool { return d.SetupSentAt != "" || d.SetupDoneAt != "" }

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
	page := s.page(b)
	defer page.Close()
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
	// The quota and the limits are read side by side; limits read recently
	// are kept, since reading them may mean scanning Muse's scripts.
	var caps *MuseCapabilities
	var wg sync.WaitGroup
	if !capabilitiesFresh(data.Capabilities, time.Now()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer logger.Recover("đọc giới hạn Muse")
			caps, _ = page.Capabilities(ctx)
		}()
	}
	quota, _ := page.Quota(ctx)
	wg.Wait()
	if quota != nil {
		data.Quota = quota
	}
	if caps != nil {
		data.Capabilities = caps
	}
	resume := quotaResumes(data, quota)
	if resume {
		data.RestReason = ""
	}
	return accounts.Inspection{SignedIn: true, Usage: accountUsage(data), Data: data, Resume: resume}, nil
}

// AfterSignIn sends the setup message in a chat of its own once the user has
// signed in in the window and the quota was read, once per account: an
// account Muse took it from never sends it again (a send Muse did not take is
// tried at the next sign-in). Muse replying is all it takes; the chat is left
// as it is. A failure is only noted on the account: it is signed in either way.
func (s *Site) AfterSignIn(ctx context.Context, b *browser.Browser, rec accounts.Record, insp accounts.Inspection) accounts.Inspection {
	cfg := s.cfg.Get()
	if strings.TrimSpace(cfg.Muse.SetupMessage) == "" {
		return insp
	}
	data, ok := insp.Data.(museData)
	if !ok || data.setupSent() {
		return insp
	}
	// Muse may take a while to reply; the sign-in window's own deadline must
	// not cut the wait short.
	limit := time.Duration(cfg.Muse.SendTimeoutSeconds)*time.Second + time.Duration(cfg.Muse.ImageTimeoutMinutes)*time.Minute
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), limit)
	defer cancel()
	page := s.page(b)
	defer page.Close()
	reply, sent, err := sendSetup(ctx, page, cfg.Muse, sleepContext, time.Now)
	if sent {
		// A send like a job's: the next job on the account keeps its send gap.
		data.LastSentAt = time.Now().UTC().Format(time.RFC3339Nano)
		data.SetupSentAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err != nil {
		logger.Get().Warn("muse setup message failed", "account", rec.ID, "sent", sent, "error", err)
		insp.Note = "Chưa gửi được tin nhắn thiết lập cho Muse: " + err.Error() + "."
		if sent {
			insp.Note = "Muse đã nhận tin nhắn thiết lập nhưng chưa trả lời (" + err.Error() + "); app không gửi lại."
		}
	} else {
		data.SetupDoneAt, data.SetupReply = time.Now().UTC().Format(time.RFC3339), reply
		logger.Get().Info("muse setup message answered", "account", rec.ID)
	}
	insp.Data, insp.Usage = data, accountUsage(data)
	return insp
}

// page is the account's muse.ai tab for a check or a sign-in.
func (s *Site) page(b *browser.Browser) *chromePage {
	cfg := s.cfg.Get()
	return &chromePage{
		port: b.Port, policy: cfg.Accounts,
		callTimeout: time.Duration(cfg.Accounts.RequestTimeoutSeconds) * time.Second,
		sendTimeout: time.Duration(cfg.Muse.SendTimeoutSeconds) * time.Second,
	}
}

// accountUsage is the account card: the quota, then the setup reply's time.
func accountUsage(data museData) []accounts.UsageItem {
	items := quotaUsage(data.Quota)
	if data.SetupDoneAt != "" {
		value := "Muse đã trả lời"
		if t, err := time.Parse(time.RFC3339, data.SetupDoneAt); err == nil {
			value += " " + t.Local().Format("15:04 02/01")
		}
		items = append(items, accounts.UsageItem{Label: "Tin nhắn thiết lập", Value: value})
	}
	return items
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
