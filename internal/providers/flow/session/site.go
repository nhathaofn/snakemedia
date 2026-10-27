// Package session connects Google Flow to the shared accounts service: the
// Flow page as an accounts.Site, and the account view the Flow provider works
// with (session values, balance) kept in each account's provider data.
package session

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/providers/flow/chrome"
)

// flowData is what a Flow account keeps beside the shared fields. It never
// crosses the Wails bridge.
type flowData struct {
	GaiaID         string              `json:"gaiaId"`
	Credentials    chrome.Credentials  `json:"credentials"`
	Balance        *chrome.FlowCredits `json:"balance,omitempty"`
	BalanceError   string              `json:"balanceError,omitempty"`
	LastVerifiedAt string              `json:"lastVerifiedAt,omitempty"`
}

// Site is Google Flow for the accounts service.
type Site struct {
	cfg     *config.Manager
	inspect func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error)
}

// NewSite reads Flow sessions with the shared account policy.
func NewSite(cfg *config.Manager) *Site {
	return &Site{cfg: cfg, inspect: chrome.InspectFlowSession}
}

func (*Site) Provider() accounts.Provider { return accounts.ProviderFlow }

func (*Site) Page() browser.Site {
	return browser.Site{
		Home: chrome.FlowURL,
		Owns: func(u *url.URL) bool { return u.Scheme == "https" && u.Host == chrome.FlowHost },
	}
}

func (*Site) CookieDomain() string { return ".google.com" }

// AcceptsCookie keeps Google's own cookies: the account session lives on
// google.com and Flow runs on flow.google.com.
func (*Site) AcceptsCookie(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	return d == "google.com" || strings.HasSuffix(d, ".google.com")
}

// Inspect reads the signed-in Google identity, the Flow session values and
// the balance from the account's Flow tab. A balance error keeps the account
// signed in and is shown beside its usage.
func (s *Site) Inspect(ctx context.Context, b *browser.Browser, rec accounts.Record) (accounts.Inspection, error) {
	var data flowData
	if err := accounts.DecodeData(rec, &data); err != nil {
		return accounts.Inspection{}, err
	}
	details, err := s.inspect(ctx, b.Port, s.cfg.Get().FlowPagePolicy())
	if err != nil {
		return accounts.Inspection{}, err
	}
	if details == nil || !details.IsLoggedIn {
		return accounts.Inspection{}, nil
	}
	if data.GaiaID != "" && data.GaiaID != details.GaiaID {
		return accounts.Inspection{}, errors.New("Chrome đang đăng nhập tài khoản Google khác. Dữ liệu tài khoản được giữ nguyên.")
	}
	data.GaiaID = details.GaiaID
	data.Credentials = keepRPCs(details.Credentials, data.Credentials)
	if details.CreditsVerified {
		data.Balance = &details.Credits
		data.BalanceError = ""
		data.LastVerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
	} else if details.CreditsErr != nil {
		data.BalanceError = details.CreditsErr.Error()
	}
	return accounts.Inspection{SignedIn: true, Identity: details.Email, Usage: usage(data), Data: data}, nil
}

// keepRPCs keeps the discovered RPC IDs when an identity read of the same
// build did not discover them again.
func keepRPCs(fresh, stored chrome.Credentials) chrome.Credentials {
	if fresh.RPCs == (chrome.RPCRegistry{}) && fresh.BuildLabel == stored.BuildLabel {
		fresh.RPCs = stored.RPCs
	}
	return fresh
}

// usage words the balance for the account card.
func usage(data flowData) []accounts.UsageItem {
	if data.Balance == nil {
		items := []accounts.UsageItem{{Label: "Credits", Value: "Chưa xác minh"}}
		if data.BalanceError != "" {
			items = append(items, accounts.UsageItem{Label: "Lỗi số dư", Value: data.BalanceError})
		}
		return items
	}
	b := data.Balance
	plan := string(b.Tier)
	if b.Tier == chrome.TierUnknown {
		plan = fmt.Sprintf("Mã gói %d", b.TierCode)
	}
	items := []accounts.UsageItem{{Label: "Credits", Value: strconv.Itoa(b.Credits)}, {Label: "Gói", Value: plan}}
	if b.DailyLimit > 0 {
		items = append(items, accounts.UsageItem{Label: "Hạn mức ngày", Value: strconv.Itoa(b.DailyLimit)})
	}
	if reset, err := time.Parse(time.RFC3339, b.ResetAt); err == nil {
		items = append(items, accounts.UsageItem{Label: "Đặt lại", Value: reset.Local().Format("15:04 02/01")})
	}
	if data.BalanceError != "" {
		items = append(items, accounts.UsageItem{Label: "Lỗi số dư", Value: data.BalanceError})
	}
	return items
}
