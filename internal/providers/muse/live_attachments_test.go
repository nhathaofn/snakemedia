package muse

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/paths"
	"github.com/google/uuid"
)

type liveAttachmentPage struct {
	Page
	t       *testing.T
	message string
	sends   int
	result  SendResult
}

func (p *liveAttachmentPage) Send(ctx context.Context, text string, atts []Attachment) (SendResult, error) {
	p.sends++
	if text != p.message {
		p.t.Fatal("the production runner changed the saved prompt or its app guide")
	}
	if size := inlineRequestBytes(text, atts); size > museInlineRequestBudget {
		p.t.Fatalf("request too large: %d", size)
	}
	p.t.Logf("production send: attachments=%d messageBytes=%d requestBytes=%d messageSHA256=%x", len(atts), len(text), inlineRequestBytes(text, atts), sha256.Sum256([]byte(text)))
	res, err := p.Page.Send(ctx, text, atts)
	p.result = res
	p.t.Logf("send: ok=%v session=%q reason=%q diagnostics=%+v error=%v", res.OK, res.SessionID, res.Reason, res.Diagnostics, err)
	return res, err
}

// TestLiveAttachments reruns saved send_failed jobs with their full app guide,
// roles, references and original prompt through execute -> attachments -> send.
// It never starts the saved queue. Local test journals/output live in TempDir.
// Opt in with SNAKEMEDIA_LIVE_ATTACHMENTS=1; this spends real Muse turns.
func TestLiveAttachments(t *testing.T) {
	if os.Getenv("SNAKEMEDIA_LIVE_ATTACHMENTS") != "1" {
		t.Skip("set SNAKEMEDIA_LIVE_ATTACHMENTS=1 to rerun real Muse prompts")
	}
	ps, err := paths.Resolve("SnakeMedia")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewManager(ps.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	accts := accounts.New(ps.AccountsDir, cfg, NewSite(cfg))
	accts.Start(ctx, func(events.Event, any) {})
	defer accts.Close()
	var journal struct {
		Records []MuseJob `json:"records"`
	}
	data, err := os.ReadFile(filepath.Join(ps.DataDir, "muse", "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, saved := range journal.Records {
		count := len(saved.References) + len(saved.Frames)
		if count < 5 || seen[count] || !strings.Contains(saved.Error, "send_failed") {
			continue
		}
		seen[count] = true
		t.Run(fmt.Sprintf("%d-images", count), func(t *testing.T) {
			rec, err := accts.Record(saved.AccountID)
			if err != nil || rec.Status != accounts.StatusSignedIn || !rec.Enabled {
				recs, listErr := accts.Records(accounts.ProviderMuse)
				if listErr != nil {
					t.Fatal(listErr)
				}
				rec = accounts.Record{}
				for _, candidate := range recs {
					if candidate.Enabled && candidate.Status == accounts.StatusSignedIn {
						rec = candidate
						break
					}
				}
				if rec.ID == "" {
					t.Fatal("no enabled signed-in Muse account")
				}
				t.Log("original account unavailable; using another signed-in account with the exact saved message")
			}
			var d museData
			if err := accounts.DecodeData(rec, &d); err != nil {
				t.Fatal(err)
			}
			t.Logf("savedJob=%s setupSent=%v setupReplyReceived=%v setupMessageBytes=%d", saved.ID, d.setupSent(), d.SetupDoneAt != "", len(cfg.Get().Muse.SetupMessage))
			p := New(t.TempDir(), t.TempDir(), cfg, media.NewStore(ps.MediaDir), accts)
			p.ctx = ctx
			if err := p.jobs.load(); err != nil {
				t.Fatal(err)
			}
			job := saved
			resetForRetry(&job)
			job.ID, job.Attempts, job.OutputDir = uuid.NewString(), 0, ""
			job.CreatedAt = time.Now().UTC().Format(time.RFC3339)
			if _, err := p.jobs.add(job); err != nil {
				t.Fatal(err)
			}
			var tracked *liveAttachmentPage
			p.pageFor = func(ctx context.Context, rec accounts.Record) (Page, error) {
				page, err := p.openPage(ctx, rec)
				if err != nil {
					return nil, err
				}
				t.Cleanup(func() { closePage(page) })
				tracked = &liveAttachmentPage{Page: page, t: t, message: buildMessage(saved)}
				return tracked, nil
			}
			runCtx, stop := context.WithTimeout(ctx, 90*time.Second)
			defer stop()
			err = p.execute(runCtx, job.ID, rec.ID)
			got, ok := p.jobs.get(job.ID)
			if tracked == nil || tracked.sends != 1 || !tracked.result.OK || !ok || got.SessionID == "" {
				t.Fatalf("not accepted: job=%s session=%q error=%v", job.ID, got.SessionID, err)
			}
			t.Logf("production runner persisted session=%s; generation result=%v", got.SessionID, err)
			status, statusErr := tracked.Status(ctx, got.SessionID)
			replies, replyErr := tracked.Result(ctx, got.SessionID)
			t.Logf("chat verified: status=%q statusError=%v assistantReplies=%d historyError=%v", status, statusErr, len(replies), replyErr)
			if statusErr != nil || status == "" || replyErr != nil {
				t.Fatal("could not verify the accepted chat")
			}
		})
	}
	if len(seen) == 0 {
		t.Fatal("no saved failed job with at least five references")
	}
}
