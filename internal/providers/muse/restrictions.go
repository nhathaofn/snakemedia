package muse

import (
	"fmt"
	"net/url"
	"strings"
)

type accountRestriction int

const (
	restrictNone accountRestriction = iota
	// restrictDeadAccount means the account itself is dead, banned, suspended or
	// cannot enter muse.ai at all.
	restrictDeadAccount
	// restrictVideoDisabled means the account can sign in, but cannot create videos
	// (video capability banned/disabled/failed verification).
	restrictVideoDisabled
	// restrictDailyVideo24h means Muse rejected video creation due to the daily
	// video limit; the account rests for 24h and auto-re-enables afterwards.
	restrictDailyVideo24h
)

type accountRestrictedError struct {
	dead   bool
	reason string
}

func (e *accountRestrictedError) Error() string {
	if e.dead {
		if e.reason != "" {
			return "Tài khoản Muse đã chết hoặc bị khóa: " + e.reason
		}
		return "Tài khoản Muse đã chết hoặc bị khóa."
	}
	if e.reason != "" {
		return "Tài khoản Muse bị cấm hoặc không tạo được video: " + e.reason
	}
	return "Tài khoản Muse bị cấm hoặc không tạo được video."
}

type dailyVideoLimitError struct {
	reason string
}

func (e *dailyVideoLimitError) Error() string {
	if e.reason != "" {
		return "Tài khoản đã hết hạn tạo video trong ngày (nghỉ 24h): " + e.reason
	}
	return "Tài khoản đã hết hạn tạo video trong ngày (nghỉ 24h)."
}

type awayTabInfo struct {
	URL   string `json:"url"`
	Ready string `json:"ready"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

func (s LoginState) blockedReason() string {
	status := strings.ToLower(strings.TrimSpace(s.Status))
	switch status {
	case "banned", "suspended", "disabled", "blocked", "terminated", "deleted", "locked", "restricted",
		"waitlist", "waitlisted", "waiting_list", "verification_required", "verify_required",
		"unverified_account", "phone_verification", "identity_verification":
		if s.Error != "" {
			return s.Error
		}
		return "trạng thái phiên: " + status
	}
	path := strings.ToLower(strings.TrimSpace(s.Path))
	for _, seg := range []string{
		"/banned", "/suspended", "/blocked", "/disabled", "/terminated", "/account-locked",
		"/waitlist", "/waiting-list", "/verify", "/verification", "/verify-account", "/identity-verification", "/account-review",
	} {
		if strings.Contains(path, seg) {
			return firstNonEmpty(s.Notice, "trang chuyển hướng "+s.Path)
		}
	}
	if s.HTTP == 403 {
		return firstNonEmpty(s.Error, s.Notice, "HTTP 403 Forbidden")
	}
	if kind, detail := classifyText(s.Error, "", s.Notice, nil); kind == restrictDeadAccount {
		return detail
	}
	return ""
}

// isOAuthOrLoginBounce reports whether an away tab looks like a transient or
// interactive login / OAuth page (as opposed to a permanently blocked/waitlist screen).
func isOAuthOrLoginBounce(info awayTabInfo) bool {
	u, err := url.Parse(strings.TrimSpace(info.URL))
	if err != nil || info.URL == "" || info.URL == "about:blank" || strings.HasPrefix(info.URL, "chrome://") {
		return false
	}
	lowerURL := strings.ToLower(sanitizeURL(u))
	for _, hint := range []string{"/login", "/signin", "/sign-in", "/oauth", "/api/auth", "/auth/callback", "accounts.google.com"} {
		if strings.Contains(lowerURL, hint) {
			return true
		}
	}
	return false
}

// classifyAwayTab inspects a browser tab that is not on muse.ai after navigation.
// Returns restrictDeadAccount when the redirect clearly indicates an inaccessible
// or dead account rather than an ordinary sign-in prompt.
func classifyAwayTab(info awayTabInfo) (accountRestriction, string) {
	rawURL := strings.TrimSpace(info.URL)
	u, err := url.Parse(rawURL)
	if err != nil || rawURL == "" || rawURL == "about:blank" ||
		strings.HasPrefix(rawURL, "chrome://") || strings.HasPrefix(rawURL, "chrome-error://") {
		return restrictNone, ""
	}
	if u.Scheme == "https" && (u.Host == "muse.ai" || u.Host == "www.muse.ai") {
		return restrictNone, ""
	}
	displayURL := sanitizeURL(u)
	joined := strings.ToLower(displayURL + " " + info.Title + " " + info.Text)
	// Anti-bot / CDN challenges and browser network error screens are transient,
	// never permanent account bans.
	for _, transientMarker := range []string{
		"challenges.cloudflare.com", "/cdn-cgi/challenge", "just a moment",
		"attention required!", "checking your browser", "cloudflare", "turnstile",
		"hcaptcha", "recaptcha", "verify you are human",
	} {
		if strings.Contains(joined, transientMarker) {
			return restrictNone, ""
		}
	}
	for _, marker := range []string{
		"banned", "suspended", "disabled", "terminated", "deactivated",
		"blocked", "restricted", "couldn't find your", "account deleted",
		"waitlist", "waiting list", "join the waitlist", "early access",
		"verify your account", "verify your identity", "verification required",
		"account verification", "phone verification", "identity verification",
		"khóa", "vô hiệu", "bị cấm", "xác minh tài khoản", "danh sách chờ", "bị bắt xác minh",
		"access denied", "403",
	} {
		if strings.Contains(joined, marker) {
			return restrictDeadAccount, fmt.Sprintf("chuyển hướng sang %s (%s)", displayURL, firstNonEmpty(info.Title, marker))
		}
	}
	// An ordinary sign-in screen (OAuth / login page) on an expired session is
	// signed out, not necessarily dead. Unknown external redirects stay non-fatal.
	for _, loginHint := range []string{"/login", "/signin", "/sign-in", "/oauth", "/api/auth", "accounts.google.com"} {
		if strings.Contains(strings.ToLower(displayURL), loginHint) {
			return restrictNone, displayURL
		}
	}
	if strings.Contains(strings.ToLower(info.Title+" "+info.Text), "sign in") ||
		strings.Contains(strings.ToLower(info.Title+" "+info.Text), "log in") {
		return restrictNone, displayURL
	}
	return restrictNone, ""
}

func sanitizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	out := u.Scheme + "://" + u.Host + u.Path
	if len(out) > 120 {
		return out[:120] + "…"
	}
	return out
}

func classifyRestriction(reason string, diag *SendDiagnostics, reply string, quota *MuseQuota) (accountRestriction, string) {
	var diagMsg, diagCode string
	if diag != nil {
		diagMsg = diag.Message
		diagCode = fmt.Sprint(diag.Code)
		if diag.Code == nil {
			diagCode = ""
		}
	}
	quotaText := ""
	if quota != nil {
		quotaText = strings.TrimSpace(quota.QuotaStatus + " " + quota.Subtitle)
	}
	return classifyText(
		strings.TrimSpace(reason+" "+diagMsg+" "+diagCode),
		quotaText,
		reply,
		quota,
	)
}

func classifyRestrictionForKind(kind MuseJobKind, reason string, diag *SendDiagnostics, reply string, quota *MuseQuota) (accountRestriction, string) {
	cKind, detail := classifyRestriction(reason, diag, reply, quota)
	if cKind != restrictNone {
		return cKind, detail
	}
	if kind == MuseJobVideo {
		allLower := strings.ToLower(strings.TrimSpace(reason + " " + diagText(diag) + " " + reply))
		if matchesRegionRestriction(allLower) || matchesVideoCapabilityRestriction(allLower) {
			return restrictVideoDisabled, firstNonEmpty(truncate(strings.TrimSpace(reply), 200), truncate(strings.TrimSpace(reason), 180))
		}
	}
	return restrictNone, ""
}

func diagText(diag *SendDiagnostics) string {
	if diag == nil {
		return ""
	}
	if diag.Code == nil {
		return diag.Message
	}
	return strings.TrimSpace(diag.Message + " " + fmt.Sprint(diag.Code))
}

func matchesRegionRestriction(lower string) bool {
	for _, phrase := range []string{
		"not available in your region", "not available in your country", "not available in your location", "not available in your area",
		"unavailable in your region", "unavailable in your country", "unavailable in your location", "unavailable in your area",
		"not supported in your region", "not supported in your country", "not supported in your location",
		"region restricted", "region locked", "region_blocked", "geo_blocked", "geo-restricted", "georestricted",
		"country restricted", "unsupported region", "unsupported_region", "not_available_in_region",
		"regional restriction", "not available where you are", "only available in selected regions",
		"không khả dụng tại khu vực", "không khả dụng ở khu vực", "không hỗ trợ tại quốc gia", "không hỗ trợ ở khu vực",
		"giới hạn vùng", "giới hạn khu vực", "vùng miền không được hỗ trợ", "khu vực của bạn không hỗ trợ",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	hasGeo := strings.Contains(lower, "region") || strings.Contains(lower, "country") ||
		strings.Contains(lower, "location") || strings.Contains(lower, "khu vực") || strings.Contains(lower, "quốc gia")
	hasUnavail := strings.Contains(lower, "not available") || strings.Contains(lower, "unavailable") ||
		strings.Contains(lower, "not supported") || strings.Contains(lower, "restricted") ||
		strings.Contains(lower, "không hỗ trợ") || strings.Contains(lower, "không khả dụng")
	return hasGeo && hasUnavail
}

func matchesVideoCapabilityRestriction(lower string) bool {
	for _, phrase := range []string{
		"only generate images", "only create images", "can only generate images", "can only create images",
		"image generation only", "images only", "chỉ tạo được ảnh", "chỉ hỗ trợ tạo ảnh",
		"video_disabled", "video_banned", "feature_disabled",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	mentionsVideo := strings.Contains(lower, "video")
	if !mentionsVideo {
		return false
	}
	for _, phrase := range []string{
		"not enabled", "isn't enabled", "not supported", "isn't supported", "not available", "isn't available",
		"currently unavailable", "no longer available", "requires a paid", "need a paid plan", "upgrade your plan",
		"upgrade to create", "upgrade to generate", "not allowed", "disabled", "restricted",
		"không khả dụng", "không hỗ trợ", "tạm khóa", "chưa được bật", "nâng cấp gói",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// isSessionOrGatewayFailureText reports whether an error/refusal text points to
// an expired session, dropped gateway WebSocket, or transient tab readiness issue
// rather than an account ban, quota limit, or prompt policy refusal.
func isSessionOrGatewayFailureText(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"not_ready", "gateway", "disconnected", "unauthorized", "unauthenticated",
		"session expired", "session_expired", "invalid session", "token expired",
		"not logged in", "not signed in", "login required", "auth_required",
		"http 401", "status 401", "401 unauthorized", "socket closed", "connection closed",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func classifyText(sysMsg, quotaMsg, reply string, _ *MuseQuota) (accountRestriction, string) {
	systemLower := strings.ToLower(strings.TrimSpace(sysMsg + " " + quotaMsg))
	replyLower := strings.ToLower(strings.TrimSpace(reply))
	allLower := strings.TrimSpace(systemLower + " " + replyLower)
	if allLower == "" {
		return restrictNone, ""
	}
	detail := firstNonEmpty(truncate(strings.TrimSpace(sysMsg), 180), truncate(strings.TrimSpace(quotaMsg), 180), truncate(strings.TrimSpace(reply), 200))

	// 1. Account banned / suspended / dead / verification / waitlist.
	// Explicit account notices match either system or reply text; ambiguous bare
	// words ("waitlist", "verify") only match trusted system/quota metadata so
	// LLM prompt refusals cannot disable healthy accounts.
	for _, phrase := range []string{
		"account banned", "account_banned", "account is banned", "account has been banned",
		"account suspended", "account_suspended", "account is suspended", "account has been suspended",
		"account disabled", "account_disabled", "account is disabled", "account has been disabled",
		"account terminated", "account_terminated", "account deactivated", "permanently banned",
		"your account has been", "tài khoản bị khóa", "tài khoản đã bị cấm",
		"on the waitlist", "join the waitlist", "you're on the waitlist",
		"verify your account", "account verification",
		"verification required for your account", "requires verification for your account",
		"complete verification for your account",
		"xác minh tài khoản", "danh sách chờ", "bắt xác minh",
	} {
		if strings.Contains(allLower, phrase) {
			return restrictDeadAccount, detail
		}
	}
	if strings.Contains(allLower, "your account") &&
		(strings.Contains(allLower, "verification required") || strings.Contains(allLower, "complete verification")) {
		return restrictDeadAccount, detail
	}
	for _, sysOnly := range []string{
		"waitlist", "waitlisted", "waiting list",
		"verify your identity", "verification required", "requires verification",
		"phone verification", "identity verification", "complete verification", "cần xác minh",
	} {
		if strings.Contains(systemLower, sysOnly) {
			return restrictDeadAccount, detail
		}
	}
	if strings.Contains(systemLower, "forbidden") || strings.Contains(systemLower, "403") {
		if strings.Contains(systemLower, "banned") || strings.Contains(systemLower, "suspended") ||
			strings.Contains(systemLower, "disabled") || strings.Contains(systemLower, "waitlist") || strings.Contains(systemLower, "verify") {
			return restrictDeadAccount, detail
		}
	}

	// 2. Daily video generation limit (24h).
	for _, phrase := range []string{
		"daily video", "daily_video", "daily_limit", "daily generation limit", "daily video cap",
		"reached your daily", "you have reached your daily", "you've reached your daily",
		"try again in 24", "try again tomorrow", "come back tomorrow",
		"video_limit", "video generation limit",
		"hết lượt tạo video", "hết hạn tạo video", "giới hạn tạo video trong ngày", "hết lượt video hôm nay",
	} {
		if strings.Contains(allLower, phrase) {
			return restrictDailyVideo24h, detail
		}
	}
	for _, sysOnly := range []string{
		"videos per day", "video per day", "daily limit", "daily quota",
		"limit for today", "today's limit", "reached today",
		"24 hours", "24-hour", "24 hour", "24h limit", "for the next 24", "within 24 hour", "wait 24",
		"too many videos",
	} {
		if strings.Contains(systemLower, sysOnly) {
			return restrictDailyVideo24h, detail
		}
	}
	if strings.Contains(systemLower, "limit") &&
		(strings.Contains(systemLower, "today") || strings.Contains(systemLower, "24h") || strings.Contains(systemLower, "24 hour") || strings.Contains(systemLower, "per day")) {
		return restrictDailyVideo24h, detail
	}

	// 3. Video generation banned/disabled/region-locked for this account.
	for _, phrase := range []string{
		"video generation is disabled", "video creation is disabled", "video generation disabled",
		"video generation is not available", "video creation is not available", "video feature is not available",
		"video generation isn't available", "video creation isn't available",
		"video generation unavailable", "video creation unavailable",
		"video tool is unavailable", "video tool is disabled", "video tool is not available",
		"disabled for your account", "disabled for this account",
		"banned from generating video", "banned from creating video",
		"restricted from generating video", "restricted from creating video",
		"cannot generate videos on this account", "not allowed to generate video",
		"no access to video", "don't have access to video", "do not have access to video",
		"unable to generate video", "unable to create video", "can't generate video", "cannot create video",
		"video_disabled", "video_banned", "generation_disabled",
		"cấm tạo video", "bị hạn chế tạo video", "không tạo được video", "không hỗ trợ tạo video", "không khả dụng để tạo video",
	} {
		if strings.Contains(allLower, phrase) {
			return restrictVideoDisabled, detail
		}
	}
	if strings.Contains(allLower, "video") && matchesRegionRestriction(allLower) {
		return restrictVideoDisabled, detail
	}
	if quotaMsg != "" {
		qLower := strings.ToLower(quotaMsg)
		for _, kw := range []string{"banned", "suspended", "disabled", "restricted", "blocked", "waitlist", "verify"} {
			if strings.Contains(qLower, kw) {
				if strings.Contains(qLower, "video") {
					return restrictVideoDisabled, detail
				}
				return restrictDeadAccount, detail
			}
		}
	}
	return restrictNone, ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
