package muse

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

// MuseURL is the page every account browser keeps open. It is a wire protocol
// identifier, never captured session data.
const MuseURL = "https://muse.ai/"

// chunkSize is how much of a result file one page call returns (base64-encoded).
const chunkSize = 2 << 20

// LoginState is what the page reports about its own session.
type LoginState struct {
	LoggedIn bool   `json:"loggedIn"`
	HTTP     int    `json:"http"`
	Status   string `json:"status"`
	VM       string `json:"vm"`
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
	// Send posts text and attachments into the chat thread (a session ID), or
	// starts a new chat when thread is empty.
	Send(ctx context.Context, text string, atts []Attachment, thread string) (SendResult, error)
	// Status returns the session status; "running" while Muse works.
	Status(ctx context.Context, sessionID string) (string, error)
	Result(ctx context.Context, sessionID string) ([]AssistantEvent, error)
	// Download streams a sandbox file into sink and returns the bytes written.
	Download(ctx context.Context, path string, sink io.Writer) (int64, error)
	Cancel(ctx context.Context, sessionID string) error
	// Quota returns nil when Muse's subscription action cannot be found.
	Quota(ctx context.Context) (*MuseQuota, error)
}

// MuseQuota is an account's subscription usage as Muse reports it.
type MuseQuota struct {
	PercentUsed float64 `json:"percentUsed"`
	// ResetsAt is when the weekly usage resets (RFC 3339), empty when unknown.
	ResetsAt     string  `json:"resetsAt,omitempty"`
	Status       string  `json:"status"`
	Tier         string  `json:"tier"`
	Paid         bool    `json:"paid"`
	TopupBalance float64 `json:"topupBalance"`
	TopupTotal   float64 `json:"topupTotal"`
	Label        string  `json:"label"`
	Subtitle     string  `json:"subtitle"`
	CheckedAt    string  `json:"checkedAt"`
}

// subscription is the raw object returned by fetchSubscriptionAction.
type subscription struct {
	Usage *struct {
		PercentUsed float64 `json:"percentUsed"`
		ResetsAt    float64 `json:"resetsAt"`
		QuotaStatus string  `json:"quotaStatus"`
	} `json:"usage"`
	Tier *struct {
		Name   string `json:"name"`
		IsPaid bool   `json:"isPaid"`
	} `json:"tier"`
	TopupBalance       float64 `json:"topupBalance"`
	TopupTotal         float64 `json:"topupTotal"`
	UsageRowValueLabel string  `json:"usageRowValueLabel"`
	StatusSubtitle     string  `json:"statusSubtitle"`
}

func parseQuota(sub *subscription, now time.Time) *MuseQuota {
	if sub == nil || sub.Usage == nil {
		return nil
	}
	q := &MuseQuota{
		PercentUsed: sub.Usage.PercentUsed, Status: sub.Usage.QuotaStatus,
		TopupBalance: sub.TopupBalance, TopupTotal: sub.TopupTotal,
		Label: sub.UsageRowValueLabel, Subtitle: sub.StatusSubtitle,
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

// exhaustedStatus matches quota statuses meaning the weekly quota is used up;
// SUFFICIENT and LOW still have room.
var exhaustedStatus = regexp.MustCompile(`(?i)EXHAUST|EXCEED|DEPLET|LIMIT|OUT|BLOCK`)

// weeklyOut reports whether the weekly quota is used up.
func (q *MuseQuota) weeklyOut() bool {
	return q != nil && (q.PercentUsed >= 100 || exhaustedStatus.MatchString(q.Status))
}

func (q *MuseQuota) resetsAt() time.Time {
	if q == nil || q.ResetsAt == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, q.ResetsAt)
	return t
}

func (q *MuseQuota) topupLeft() bool {
	return q != nil && q.TopupTotal > 0 && q.TopupBalance > 0
}

// Muse's own replies, matched when it produced no file. Usage replies rest the
// account; policy replies need a different prompt and are never retried.
var (
	usageReply  = regexp.MustCompile(`(?i)hết (lượt|hạn mức|quota|giới hạn)|đạt (giới hạn|hạn mức)|vượt (quá )?(giới hạn|hạn mức)|quá nhiều (yêu cầu|lượt|video)|giới hạn (sử dụng|tạo|hằng ngày|hàng ngày)|xác minh (tài khoản|danh tính)|usage limit|rate limit|reached (your|the|a) (daily |weekly |usage )?limit|limit reached|out of (credits|generations|quota|tokens)|too many requests|verify (your|that)`)
	policyReply = regexp.MustCompile(`(?i)không thể hỗ trợ|chính sách|vi phạm|không phù hợp|không được phép|tiêu chuẩn cộng đồng|đổi prompt|thay đổi prompt|safety policy|content policy|usage polic|violat|guideline|can'?t (help|create|make)|cannot (help|create|make)|not able to (help|create|make)|won'?t (be able to )?(create|make)`)
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

// errSendUnknown means a send failed in transport: Muse may or may not have
// started a generation, so the job must not be sent again automatically.
var errSendUnknown = errors.New("Không rõ Muse đã nhận lệnh hay chưa (mất kết nối với trang khi đang gửi). Kiểm tra lịch sử chat trên Muse; nếu chưa có thì bấm Chạy lại.")

// errNotSignedIn means the account's page is not signed in; the job goes back
// to the queue without spending an attempt.
var errNotSignedIn = errors.New("Tài khoản chưa đăng nhập Muse.")

// classifyReply turns Muse's text answer (when no file came back) into an error.
func classifyReply(reply, what string) error {
	reply = truncate(strings.Join(strings.Fields(reply), " "), 300)
	switch {
	case reply == "":
		return &museError{msg: "Muse không trả về " + what + "."}
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
