package muse

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
)

func setupConfig() config.MuseConfig {
	cfg := config.DefaultMuseConfig()
	cfg.PollIntervalMS = 1000
	return cfg
}

func noSleep(context.Context, time.Duration) error { return nil }

// The setup message goes out once in its own chat (no attachments) and Muse
// replying is what counts; the reply is kept.
func TestSendSetupWaitsForMuseReply(t *testing.T) {
	page := &fakePage{
		sendRes:  SendResult{OK: true, SessionID: "s1"},
		statuses: []string{"running", "completed"},
		events:   []AssistantEvent{{Text: "  Saved to memory.  "}},
	}
	reply, sent, err := sendSetup(context.Background(), page, setupConfig(), noSleep, time.Now)
	if err != nil || !sent || reply != "Saved to memory." {
		t.Fatalf("reply %q, sent %v, err %v", reply, sent, err)
	}
	if len(page.sends) != 1 || !strings.HasPrefix(page.sends[0], "Please save the following rule") || page.attachments[0] != nil {
		t.Fatalf("sends %q %v", page.sends, page.attachments)
	}
}

// A message Muse did not take is reported as not sent (tried again at the
// next sign-in); one it took but did not answer is sent (never sent again).
// An empty message sends nothing.
func TestSendSetupReportsFailures(t *testing.T) {
	cases := []struct {
		name string
		page *fakePage
		sent bool
	}{
		{"refused", &fakePage{sendRes: SendResult{Reason: "quota"}}, false},
		{"lost", &fakePage{sendErr: errors.New("tab closed")}, false},
		{"no reply", &fakePage{sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"completed"}}, true},
	}
	for _, c := range cases {
		reply, sent, err := sendSetup(context.Background(), c.page, setupConfig(), noSleep, time.Now)
		if err == nil || reply != "" || sent != c.sent {
			t.Errorf("%s: reply %q, sent %v, err %v", c.name, reply, sent, err)
		}
	}
	cfg := setupConfig()
	cfg.SetupMessage = "  "
	page := &fakePage{}
	if reply, sent, err := sendSetup(context.Background(), page, cfg, noSleep, time.Now); err != nil || reply != "" || sent || len(page.sends) != 0 {
		t.Fatalf("empty message: %q %v %v %v", reply, sent, err, page.sends)
	}
}

// Muse not answering in time ends the wait with an error; the message was
// still taken.
func TestSendSetupGivesUpAtTheLimit(t *testing.T) {
	page := &fakePage{sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running"}}
	clock := time.Now()
	now := func() time.Time { return clock }
	sleep := func(context.Context, time.Duration) error { clock = clock.Add(time.Minute); return nil }
	if _, sent, err := sendSetup(context.Background(), page, setupConfig(), sleep, now); err == nil || !sent {
		t.Fatalf("sent %v, err %v", sent, err)
	}
}

// An account that has sent its setup message (taken or answered) never sends
// it again; the check happens before any page call, so a nil browser is fine.
func TestAfterSignInSendsOncePerAccount(t *testing.T) {
	cfg, err := config.NewManager(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	site := NewSite(cfg)
	for _, data := range []museData{{SetupSentAt: "2026-10-27T08:00:00Z"}, {SetupDoneAt: "2026-10-27T08:00:00Z"}} {
		in := accounts.Inspection{SignedIn: true, Data: data}
		if out := site.AfterSignIn(context.Background(), nil, accounts.Record{}, in); out.Note != "" || out.Data.(museData) != data {
			t.Fatalf("sent again for %+v: %+v", data, out)
		}
	}
}
