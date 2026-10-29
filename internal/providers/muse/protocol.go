package muse

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

// MuseURL is the page every account browser keeps open. It is a wire protocol
// identifier, never captured session data.
const MuseURL = "https://muse.ai/"

// chunkSize is how much of a result file one page call returns (base64-encoded).
const chunkSize = 2 << 20

// LoginState is what the page reports about its own session.
type LoginState struct {
	LoggedIn bool `json:"loggedIn"`
	// Chat reports whether Muse's chat client is on the page; a signed-in tab
	// on another route (settings, a shared link) has none until it goes home.
	Chat   bool   `json:"chat"`
	HTTP   int    `json:"http"`
	Status string `json:"status"`
	VM     string `json:"vm"`
}

// Attachment is one image sent with a prompt, in the shape Muse's client expects.
type Attachment struct {
	Filename   string `json:"filename"`
	MimeType   string `json:"mimeType"`
	DataBase64 string `json:"dataBase64"`
	Category   string `json:"category"`
}

// SendResult is Muse's definite answer to a send. A transport error instead of
// a SendResult means the outcome is unknown.
type SendResult struct {
	OK        bool   `json:"ok"`
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

// ResultMedia is one file an assistant message produced in the session sandbox.
type ResultMedia struct {
	Path    string `json:"path"`
	Mime    string `json:"mime"`
	ByteLen int64  `json:"byteLen"`
	Missing bool   `json:"missing"`
}

// AssistantEvent is one assistant message of a session.
type AssistantEvent struct {
	Text   string        `json:"text"`
	Images []ResultMedia `json:"images"`
	Videos []ResultMedia `json:"videos"`
}

// Page is one account's signed-in muse.ai tab. The Chrome implementation lives
// in page.go; tests use fakes.
type Page interface {
	Login(ctx context.Context) (LoginState, error)
	// Send posts text and attachments into a new chat.
	Send(ctx context.Context, text string, atts []Attachment) (SendResult, error)
	// SendTo posts text into the existing chat sessionID (a long video's
	// continue message).
	SendTo(ctx context.Context, sessionID, text string) (SendResult, error)
	// Status returns the session status; "running" while Muse works.
	Status(ctx context.Context, sessionID string) (string, error)
	Result(ctx context.Context, sessionID string) ([]AssistantEvent, error)
	// Download streams a sandbox file into sink and returns the bytes written.
	Download(ctx context.Context, path string, sink io.Writer) (int64, error)
	Cancel(ctx context.Context, sessionID string) error
	// Home loads Muse's home page, where the chat client lives.
	Home(ctx context.Context) error
	// Quota returns nil when Muse's subscription cannot be read.
	Quota(ctx context.Context) (*MuseQuota, error)
	// Capabilities returns nil when Muse's limits cannot be read.
	Capabilities(ctx context.Context) (*MuseCapabilities, error)
}

// MuseQuota is an account's subscription as Muse's settings screen shows it:
// a weekly usage that resets, plus additional (top-up) tokens. The labels are
// Muse's own wording.
type MuseQuota struct {
	Tier string `json:"tier"`
	Paid bool   `json:"paid"`
	// PercentUsed is the weekly usage; ResetsAt (RFC 3339) is when it resets.
	PercentUsed float64 `json:"percentUsed"`
	ResetsAt    string  `json:"resetsAt,omitempty"`
	QuotaStatus string  `json:"quotaStatus"`
	UsageLabel  string  `json:"usageLabel"`
	UsageValue  string  `json:"usageValue"`
	Subtitle    string  `json:"subtitle"`
	// TopupBalance of TopupTotal additional tokens are left; Muse spends them
	// once the weekly usage is gone.
	TopupBalance float64 `json:"topupBalance"`
	TopupTotal   float64 `json:"topupTotal"`
	TopupLabel   string  `json:"topupLabel"`
	TopupValue   string  `json:"topupValue"`
	CheckedAt    string  `json:"checkedAt"`
}

// subscription is the raw object fetchSubscriptionAction returns.
type subscription struct {
	Tier *struct {
		Name   string `json:"name"`
		IsPaid bool   `json:"isPaid"`
	} `json:"tier"`
	Usage *struct {
		PercentUsed float64 `json:"percentUsed"`
		ResetsAt    float64 `json:"resetsAt"`
		QuotaStatus string  `json:"quotaStatus"`
	} `json:"usage"`
	StatusSubtitle     string  `json:"statusSubtitle"`
	UsageRowLabel      string  `json:"usageRowLabel"`
	UsageRowValueLabel string  `json:"usageRowValueLabel"`
	TopupBalance       float64 `json:"topupBalance"`
	TopupTotal         float64 `json:"topupTotal"`
	TopupRowLabel      string  `json:"topupRowLabel"`
	TopupRowValueLabel string  `json:"topupRowValueLabel"`
}

func parseQuota(sub *subscription, now time.Time) *MuseQuota {
	if sub == nil || sub.Usage == nil {
		return nil
	}
	q := &MuseQuota{
		PercentUsed: sub.Usage.PercentUsed, QuotaStatus: sub.Usage.QuotaStatus,
		UsageLabel: sub.UsageRowLabel, UsageValue: sub.UsageRowValueLabel, Subtitle: sub.StatusSubtitle,
		TopupBalance: sub.TopupBalance, TopupTotal: sub.TopupTotal,
		TopupLabel: sub.TopupRowLabel, TopupValue: sub.TopupRowValueLabel,
		CheckedAt: now.UTC().Format(time.RFC3339),
	}
	if sub.Usage.ResetsAt > 0 {
		q.ResetsAt = time.Unix(int64(sub.Usage.ResetsAt), 0).UTC().Format(time.RFC3339)
	}
	if sub.Tier != nil {
		q.Tier, q.Paid = sub.Tier.Name, sub.Tier.IsPaid
	}
	return q
}

// weeklyOut reports whether the weekly usage is used up.
func (q *MuseQuota) weeklyOut() bool {
	return q != nil && q.PercentUsed >= 100
}

func (q *MuseQuota) resetsAt() time.Time {
	if q == nil || q.ResetsAt == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, q.ResetsAt)
	return t
}

func (q *MuseQuota) topupLeft() bool {
	return q != nil && q.TopupBalance > 0
}

// MuseFileType is a file type Muse's composer accepts.
type MuseFileType struct {
	Mime       string   `json:"mime"`
	Extensions []string `json:"extensions"`
}

// MuseModel is a chat model the account may use.
type MuseModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MuseCapabilities is what an account may send and use, as Muse's client
// reads it: the composer's upload limits and the account's models.
type MuseCapabilities struct {
	// MaxFileBytes caps one attached file; MaxFiles caps the files of one message.
	MaxFileBytes int64          `json:"maxFileBytes"`
	MaxFiles     int            `json:"maxFiles"`
	ImageTypes   []MuseFileType `json:"imageTypes"`
	// VideoTypes are the videos the composer takes (a v2v source video);
	// limits read before they were kept have none.
	VideoTypes []MuseFileType `json:"videoTypes,omitempty"`
	Models     []MuseModel    `json:"models"`
	Model      string         `json:"model"`
	CheckedAt  string         `json:"checkedAt"`
}

// rawCapabilities is what the page script returns.
type rawCapabilities struct {
	Upload *struct {
		MaxFileSize        int64 `json:"maxFileSize"`
		MaxFilesPerUpload  int   `json:"maxFilesPerUpload"`
		SupportedMimeTypes []struct {
			MimeType   string   `json:"mimeType"`
			Extensions []string `json:"extensions"`
			Category   string   `json:"category"`
		} `json:"supportedMimeTypes"`
	} `json:"upload"`
	Models *struct {
		Selected  string `json:"selected_model"`
		Available []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"available_models"`
	} `json:"models"`
}

// parseCapabilities keeps the image types Muse accepts. Without the upload
// limits nothing can be checked, so the result is nil.
func parseCapabilities(raw rawCapabilities, now time.Time) *MuseCapabilities {
	if raw.Upload == nil || raw.Upload.MaxFileSize <= 0 || raw.Upload.MaxFilesPerUpload <= 0 {
		return nil
	}
	c := &MuseCapabilities{
		MaxFileBytes: raw.Upload.MaxFileSize, MaxFiles: raw.Upload.MaxFilesPerUpload,
		ImageTypes: []MuseFileType{}, Models: []MuseModel{},
		CheckedAt: now.UTC().Format(time.RFC3339),
	}
	for _, t := range raw.Upload.SupportedMimeTypes {
		if (t.Category == "image" || t.Category == "video") && len(t.Extensions) > 0 {
			exts := make([]string, len(t.Extensions))
			for i, e := range t.Extensions {
				exts[i] = strings.ToLower(e)
			}
			if t.Category == "image" {
				c.ImageTypes = append(c.ImageTypes, MuseFileType{Mime: t.MimeType, Extensions: exts})
			} else {
				c.VideoTypes = append(c.VideoTypes, MuseFileType{Mime: t.MimeType, Extensions: exts})
			}
		}
	}
	if raw.Models != nil {
		c.Model = raw.Models.Selected
		for _, m := range raw.Models.Available {
			c.Models = append(c.Models, MuseModel{ID: m.ID, Name: m.DisplayName})
		}
	}
	return c
}

// imageMime returns the MIME type Muse lists for a file extension.
func (c *MuseCapabilities) imageMime(ext string) (string, bool) {
	if c == nil {
		return "", false
	}
	return mimeFor(c.ImageTypes, ext)
}

// videoMime returns the MIME type Muse lists for a video file extension.
func (c *MuseCapabilities) videoMime(ext string) (string, bool) {
	if c == nil {
		return "", false
	}
	return mimeFor(c.VideoTypes, ext)
}

// mimeFor returns the MIME type of the first of types listing ext.
func mimeFor(types []MuseFileType, ext string) (string, bool) {
	ext = strings.ToLower(ext)
	for _, t := range types {
		if slices.Contains(t.Extensions, ext) {
			return t.Mime, true
		}
	}
	return "", false
}

// videoExtensions lists every accepted video extension, sorted.
func (c *MuseCapabilities) videoExtensions() []string {
	if c == nil {
		return nil
	}
	return extensionsOf(c.VideoTypes)
}

// extensions lists every accepted image extension, sorted.
func (c *MuseCapabilities) extensions() []string {
	if c == nil {
		return nil
	}
	return extensionsOf(c.ImageTypes)
}

// extensionsOf lists every extension of types, sorted and without repeats.
func extensionsOf(types []MuseFileType) []string {
	var out []string
	for _, t := range types {
		out = append(out, t.Extensions...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Errors the runner classifies. museError marks a run Muse stopped without
// the file for its own reasons (interrupted, too slow): another chat may work.
type museError struct{ msg string }

func (e *museError) Error() string { return e.msg }

// policyError is a chat Muse finished without the file while the account
// still had usage: Muse declined the prompt, so it is never retried as is.
type policyError struct{ reply string }

func (e *policyError) Error() string {
	if e.reply == "" {
		return "CẦN ĐỔI PROMPT: Muse không tạo kết quả cho prompt này."
	}
	return "CẦN ĐỔI PROMPT: Muse không tạo kết quả cho prompt này. Muse trả lời: \"" + e.reply + "\""
}

// usageError is a chat Muse finished without the file because the account's
// usage is gone (read from Muse's subscription, never from the reply).
type usageError struct{}

func (e *usageError) Error() string { return "Tài khoản hết lượt Muse." }

// notSentError is a failure before anything reached Muse (the account's
// browser, proxy or page was not ready): the job goes back to the queue
// without spending an attempt.
type notSentError struct{ err error }

func (e *notSentError) Error() string { return e.err.Error() }
func (e *notSentError) Unwrap() error { return e.err }

// errSendUnknown means a send failed in transport: Muse may or may not have
// started a generation, so the job must not be sent again automatically.
var errSendUnknown = errors.New("Không rõ Muse đã nhận lệnh hay chưa (mất kết nối với trang khi đang gửi). Kiểm tra lịch sử chat trên Muse; nếu chưa có thì bấm Chạy lại.")

// errNotSignedIn means the account's page is not signed in; the job goes back
// to the queue without spending an attempt.
var errNotSignedIn = errors.New("Tài khoản chưa đăng nhập Muse.")

// replyText is Muse's answer in a chat, on one line and shortened, for the
// message of a job that got no file.
func replyText(events []AssistantEvent) string {
	var texts []string
	for _, e := range events {
		if e.Text != "" {
			texts = append(texts, e.Text)
		}
	}
	return truncate(strings.Join(strings.Fields(strings.Join(texts, " ")), " "), 300)
}

// onceSet remembers keys already seen, so each distinct value is logged once.
type onceSet struct {
	mu   sync.Mutex
	seen map[string]bool
}

// first reports whether key is seen for the first time.
func (s *onceSet) first(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[key] {
		return false
	}
	s.seen[key] = true
	return true
}

// truncate shortens s to at most n runes without splitting a character.
func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
