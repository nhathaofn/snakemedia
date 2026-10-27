package muse

import (
	"context"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/logger"
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
	// Signals are the event's small status-like fields (path → value), such as
	// a status, an error code or a refusal flag; never message text.
	Signals map[string]string `json:"signals,omitempty"`
}

// Page is one account's signed-in muse.ai tab. The Chrome implementation lives
// in page.go; tests use fakes.
type Page interface {
	Login(ctx context.Context) (LoginState, error)
	// Send posts text and attachments into a new chat.
	Send(ctx context.Context, text string, atts []Attachment) (SendResult, error)
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
	Models       []MuseModel    `json:"models"`
	Model        string         `json:"model"`
	CheckedAt    string         `json:"checkedAt"`
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
		if t.Category == "image" && len(t.Extensions) > 0 {
			exts := make([]string, len(t.Extensions))
			for i, e := range t.Extensions {
				exts[i] = strings.ToLower(e)
			}
			c.ImageTypes = append(c.ImageTypes, MuseFileType{Mime: t.MimeType, Extensions: exts})
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
	ext = strings.ToLower(ext)
	for _, t := range c.ImageTypes {
		if slices.Contains(t.Extensions, ext) {
			return t.Mime, true
		}
	}
	return "", false
}

// extensions lists every accepted image extension, sorted.
func (c *MuseCapabilities) extensions() []string {
	var out []string
	if c == nil {
		return out
	}
	for _, t := range c.ImageTypes {
		out = append(out, t.Extensions...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Muse's own replies, matched when it produced no file. Muse's chat history
// carries no structured reason for a refusal, so its wording is matched; each
// pattern names the limit or policy itself, never a bare word ("chính sách",
// "guideline") that an ordinary answer may contain.
//   - usage replies rest the account and hand the job to another one;
//   - verification replies need the user to act in the account's Chrome;
//   - policy replies need a different prompt and are never retried.
var (
	usageReply  = regexp.MustCompile(`(?i)hết (lượt|hạn mức|quota)|đã (đạt|chạm) (giới hạn|hạn mức)|vượt (quá )?hạn mức|quá nhiều (yêu cầu|lượt)|giới hạn (sử dụng|tạo|hằng ngày|hàng ngày|hằng tuần|hàng tuần)|usage limit|rate limit|reached (your|the|a) (daily |weekly |usage )?limit|limit reached|out of (credits|generations|quota|tokens)|too many requests`)
	verifyReply = regexp.MustCompile(`(?i)xác minh (tài khoản|danh tính|số điện thoại)|verify (your )?(account|identity|phone|email)`)
	policyReply = regexp.MustCompile(`(?i)vi phạm (chính sách|tiêu chuẩn|nguyên tắc)|chính sách (nội dung|an toàn|sử dụng|cộng đồng)|tiêu chuẩn cộng đồng|nội dung không phù hợp|không được phép tạo|(đổi|thay đổi) prompt|(safety|content|usage) polic|(content|community|safety|usage) guidelines?|violates? (our|the|content|safety|usage)|can'?t (help with|create|make|generate) th|cannot (help with|create|make|generate) th|not able to (help with|create|make|generate) th|won'?t (be able to )?(create|make|generate) th`)
)

// Errors the runner classifies. museError marks an answer from Muse (no file,
// too slow): retrying in a new chat may help, re-polling the session will not.
type museError struct{ msg string }

func (e *museError) Error() string { return e.msg }

type policyError struct{ reply string }

func (e *policyError) Error() string {
	return "CẦN ĐỔI PROMPT: Muse từ chối tạo vì nội dung prompt. Muse trả lời: \"" + e.reply + "\""
}

type usageError struct{ reply string }

func (e *usageError) Error() string {
	return "Tài khoản hết lượt Muse. Muse trả lời: \"" + e.reply + "\""
}

// verifyError is Muse asking the account to verify itself: nobody but the
// user can do that, so the account stops taking jobs until it is checked again.
type verifyError struct{ reply string }

func (e *verifyError) Error() string {
	return "Muse yêu cầu xác minh tài khoản. Muse trả lời: \"" + e.reply + "\""
}

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

// replyClass is why Muse answered without a file.
type replyClass string

const (
	classPolicy    replyClass = "policy"
	classUsage     replyClass = "usage"
	classVerify    replyClass = "verify"
	classTechnical replyClass = "technical"
)

// replySignals maps the structured fields of an assistant event (see
// AssistantEvent.Signals) to a class. It is built from field names and values
// Muse-like clients use; it is not confirmed against muse.ai yet, so every
// signal it does not know is logged ("muse reply signal") and must be added
// here from the logs. Rules are tried in order; a rule matches when its key
// pattern matches the field path and its value pattern matches the value.
// A sign-in problem counts as verify: the account needs the user, and the job
// moves on without spending an attempt.
var replySignals = []struct {
	key, value *regexp.Regexp
	class      replyClass
}{
	{regexp.MustCompile(`(?i)refus|blocked|moderat`), regexp.MustCompile(`.`), classPolicy},
	{regexp.MustCompile(`.`), regexp.MustCompile(`(?i)refus|blocked|safety|moderat|content.?(policy|filter)|policy.?violation|nsfw`), classPolicy},
	{regexp.MustCompile(`.`), regexp.MustCompile(`(?i)rate.?limit|quota|usage.?limit|limit.?(exceeded|reached)|insufficient.?(credit|token|balance)|too.?many`), classUsage},
	{regexp.MustCompile(`.`), regexp.MustCompile(`(?i)verif|unauthori[sz]ed|unauthenticated|login.?required|session.?expired|forbidden`), classVerify},
	{regexp.MustCompile(`(?i)error|status|state|reason`), regexp.MustCompile(`(?i)^(error|errored|failed|failure|internal|timeout|timed.?out|aborted)$`), classTechnical},
}

// neutralSignal is a value that says nothing about a failure (a flag that is
// off, a normal finish).
var neutralSignal = regexp.MustCompile(`(?i)^(|false|0|null|none|nil|ok|success|succeeded|complete|completed|done|idle|stop|stopped|end_turn|assistant|running|finished)$`)

// signalLog remembers which signals were logged, so each distinct one appears
// once per run.
var signalLog = onceSet{seen: map[string]bool{}}

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

// signalClass classifies an event's structured fields; unknown lists the
// non-neutral signals the table does not recognise.
func signalClass(signals map[string]string) (class replyClass, unknown []string) {
	keys := make([]string, 0, len(signals))
	for k := range signals {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := strings.TrimSpace(signals[k])
		if neutralSignal.MatchString(v) {
			continue
		}
		matched := false
		for _, rule := range replySignals {
			if rule.key.MatchString(k) && rule.value.MatchString(v) {
				matched = true
				if class == "" || rank[rule.class] > rank[class] {
					class = rule.class
				}
				break
			}
		}
		if !matched {
			unknown = append(unknown, k+"="+v)
		}
	}
	return class, unknown
}

// rank orders classes when several signals disagree: the one that needs the
// user most wins, and a technical error never hides a refusal.
var rank = map[replyClass]int{classTechnical: 1, classUsage: 2, classVerify: 3, classPolicy: 4}

// classifyEvents turns a chat that produced no file into an error: from the
// events' structured signals when they name a reason, else from the reply's
// wording (classifyReply).
func classifyEvents(events []AssistantEvent, what string) error {
	var texts []string
	signals := map[string]string{}
	for _, e := range events {
		if e.Text != "" {
			texts = append(texts, e.Text)
		}
		for k, v := range e.Signals {
			signals[k] = v
		}
	}
	reply := truncate(strings.Join(strings.Fields(strings.Join(texts, "\n")), " "), 300)
	class, unknown := signalClass(signals)
	for _, s := range unknown {
		if signalLog.first("unknown:" + s) {
			logger.Get().Info("muse reply signal", "signal", s, "known", false)
		}
	}
	if class == "" {
		err := classifyReply(reply, what)
		if signalLog.first("regex:" + string(errorClass(err))) {
			logger.Get().Info("muse reply signal", "source", "wording", "class", errorClass(err), "signals", len(signals))
		}
		return err
	}
	if reply == "" {
		reply = strings.Join(signalList(signals), ", ")
	}
	switch class {
	case classPolicy:
		return &policyError{reply: reply}
	case classUsage:
		return &usageError{reply: reply}
	case classVerify:
		return &verifyError{reply: reply}
	default:
		return &museError{msg: "Muse không tạo " + what + ". Muse trả lời: \"" + reply + "\""}
	}
}

// signalList renders signals as sorted key=value pairs.
func signalList(signals map[string]string) []string {
	out := make([]string, 0, len(signals))
	for k, v := range signals {
		out = append(out, k+"="+v)
	}
	slices.Sort(out)
	return out
}

// errorClass names the class classifyReply chose, for the log.
func errorClass(err error) replyClass {
	var (
		policy *policyError
		usage  *usageError
		verify *verifyError
	)
	switch {
	case errors.As(err, &policy):
		return classPolicy
	case errors.As(err, &usage):
		return classUsage
	case errors.As(err, &verify):
		return classVerify
	default:
		return classTechnical
	}
}

// classifyReply turns Muse's text answer (when no file came back) into an
// error. It is the fallback when the chat carries no structured reason.
func classifyReply(reply, what string) error {
	reply = truncate(strings.Join(strings.Fields(reply), " "), 300)
	switch {
	case reply == "":
		return &museError{msg: "Muse không trả về " + what + "."}
	case verifyReply.MatchString(reply):
		return &verifyError{reply: reply}
	case usageReply.MatchString(reply):
		return &usageError{reply: reply}
	case policyReply.MatchString(reply):
		return &policyError{reply: reply}
	default:
		return &museError{msg: "Muse không tạo " + what + ". Muse trả lời: \"" + reply + "\""}
	}
}

// truncate shortens s to at most n runes without splitting a character.
func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
