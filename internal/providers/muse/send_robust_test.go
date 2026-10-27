package muse

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
)

// Muse answering "out of usage" moves a job on, but only a few times: each
// answer was a chat turn on some account.
func TestLimitRepliesAreCapped(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"},
		events: []AssistantEvent{{Text: "Bạn đã hết lượt tạo video hôm nay"}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	for i := 1; i < maxLimitReplies; i++ {
		p.runOne(context.Background(), job.ID, acc.ID)
		if got := mustJob(t, p, job.ID); got.Status != MuseJobQueued || got.Attempts != 0 {
			t.Fatalf("after %d limit replies: %+v", i, got)
		}
	}
	p.runOne(context.Background(), job.ID, acc.ID)
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || !strings.Contains(got.Error, "hết lượt hoặc cần xác minh") {
		t.Fatalf("job = %+v", got)
	}
	if len(page.sends) != maxLimitReplies {
		t.Fatalf("sends = %d", len(page.sends))
	}
}

// A verification request stops the account for the user, and the job waits
// for another account without spending an attempt.
func TestVerifyReplyStopsAccount(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"},
		events: []AssistantEvent{{Text: "Vui lòng xác minh tài khoản của bạn để tiếp tục."}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobQueued || got.Attempts != 0 {
		t.Fatalf("job = %+v", got)
	}
	if a, _ := p.accounts.Record(acc.ID); a.Status != accounts.StatusError || !strings.Contains(a.Message, "xác minh") {
		t.Fatalf("account = %+v", a.Account)
	}
}

func TestOrdinaryRepliesAreNotPolicyOrUsage(t *testing.T) {
	var muse *museError
	for _, reply := range []string{
		"Theo chính sách giá mới, video dài hơn sẽ đẹp hơn.",
		"Here are some guidelines for a better prompt.",
		"Kích thước này không phù hợp lắm, tôi đã chỉnh lại.",
		"Ảnh vượt quá giới hạn ký tự mô tả.",
	} {
		if err := classifyReply(reply, "ảnh"); !errors.As(err, &muse) {
			t.Errorf("%q classified as %T", reply, err)
		}
	}
	var policy *policyError
	if !errors.As(classifyReply("Yêu cầu này vi phạm chính sách nội dung.", "ảnh"), &policy) {
		t.Error("policy reply not recognised")
	}
	var verify *verifyError
	if !errors.As(classifyReply("Please verify your account to continue.", "ảnh"), &verify) {
		t.Error("verify reply not recognised")
	}
}

// A signed-in tab without the chat is sent home, never marked signed out; if
// it stays without the chat the job waits a while at no cost.
func TestPageWithoutChatIsNotSignedOut(t *testing.T) {
	page := &fakePage{loggedIn: true, noChat: true}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.Attempts != 0 || len(page.sends) != 0 {
		t.Fatalf("job = %+v, sends = %d", got, len(page.sends))
	}
	if page.homes != 1 {
		t.Fatalf("went home %d times", page.homes)
	}
	if a, _ := p.accounts.Record(acc.ID); a.Status != accounts.StatusSignedIn {
		t.Fatalf("account marked %s", a.Status)
	}
	if left := due([]MuseJob{got}, p.now()); len(left) != 0 {
		t.Fatal("a job whose page was not ready is planned again at once")
	}
	if left := due([]MuseJob{got}, p.now().Add(notSentWait+time.Second)); len(left) != 1 {
		t.Fatal("the job is held back for good")
	}
}

func TestPageWithoutChatGoesHomeAndSends(t *testing.T) {
	events, files := videoResult("/v.mp4", []byte("v"))
	page := &fakePage{loggedIn: true, noChat: true, homeFixes: true, sendRes: SendResult{OK: true, SessionID: "s1"}, events: events, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || page.homes != 1 {
		t.Fatalf("job = %+v, homes = %d", got, page.homes)
	}
}

// A page Muse refuses (HTTP 401) is signed out.
func TestRefusedSessionIsSignedOut(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if a, _ := p.accounts.Record(acc.ID); a.Status != accounts.StatusSignedOut {
		t.Fatalf("account = %s", a.Status)
	}
}

func TestRecheckWaitsLongerEachTime(t *testing.T) {
	page := &fakePage{loggedIn: true, statusErr: errors.New("socket closed")}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	p.jobs.update(job.ID, func(j *MuseJob) { j.Status, j.AccountID, j.SessionID = MuseJobProcessing, acc.ID, "s1" })

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Rechecks != 1 || !strings.HasPrefix(got.Step, "Kiểm tra lại lúc") {
		t.Fatalf("job = %+v", got)
	}
	if until, err := time.Parse(time.RFC3339Nano, got.NextCheckAt); err != nil || !until.After(p.now()) {
		t.Fatal("a failed re-check is retried at once")
	}
	if recheckDelay(1) != recheckWait || recheckDelay(2) != 2*recheckWait || recheckDelay(30) != recheckWaitMax {
		t.Fatalf("delays %v %v %v", recheckDelay(1), recheckDelay(2), recheckDelay(30))
	}
}

// Two jobs on one account never send at the same time.
func TestSendsOnOneAccountTakeTurns(t *testing.T) {
	var inFlight, overlap atomic.Int32
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}}
	page.events, page.files = videoResult("/v.mp4", []byte("v"))
	page.onSend = func() {
		if inFlight.Add(1) > 1 {
			overlap.Store(1)
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	jobs := submit(t, p, MuseJobVideo, "a", "b", "c")

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.runOne(context.Background(), j.ID, acc.ID)
		}()
	}
	wg.Wait()

	if overlap.Load() != 0 {
		t.Fatal("two sends ran together on one account")
	}
	if len(page.sends) != len(jobs) {
		t.Fatalf("sends = %d", len(page.sends))
	}
}

// A rest for a limit Muse named runs its time; a rest for the weekly quota
// ends once the quota has room.
func TestOnlyQuotaRestsEndWithTheQuota(t *testing.T) {
	room := &MuseQuota{PercentUsed: 10}
	if quotaResumes(museData{RestReason: restLimit}, room) {
		t.Error("a limit rest ended by a quota read")
	}
	if !quotaResumes(museData{RestReason: restQuota}, room) || !quotaResumes(museData{}, room) {
		t.Error("a quota rest kept although the quota has room")
	}
	if quotaResumes(museData{RestReason: restQuota}, &MuseQuota{PercentUsed: 100}) {
		t.Error("a used-up quota ended a rest")
	}

	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"},
		events: []AssistantEvent{{Text: "Too many requests, rate limit reached."}}, quota: room}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	p.runOne(context.Background(), job.ID, acc.ID)
	a, _ := p.accounts.Record(acc.ID)
	var data museData
	_ = accounts.DecodeData(a, &data)
	if data.RestReason != restLimit || a.PausedUntil == "" {
		t.Fatalf("rest = %q until %q", data.RestReason, a.PausedUntil)
	}
}

func TestWaitingSentJobSaysWhy(t *testing.T) {
	job := MuseJob{AccountID: "a", SessionID: "s", Status: MuseJobProcessing}
	cases := map[string]MuseAccount{
		"Chờ bật lại tài khoản A":   {ID: "a", Name: "A", Enabled: false, Status: accounts.StatusSignedIn},
		"Chờ tài khoản A đăng nhập": {ID: "a", Name: "A", Enabled: true, Status: accounts.StatusSignedOut},
	}
	for want, acc := range cases {
		if got := waitingStep(job, []MuseAccount{acc}); !strings.HasPrefix(got, want) {
			t.Errorf("step = %q, want %q…", got, want)
		}
	}
	if got := waitingStep(job, []MuseAccount{{ID: "a", Enabled: true, Status: accounts.StatusSignedIn}}); got != "" {
		t.Errorf("usable account: step = %q", got)
	}
}

// Limits read long ago are read again; limits that refuse a job are read
// again once before it fails.
func TestCapabilitiesAreReadAgain(t *testing.T) {
	page := &fakePage{loggedIn: true, caps: testCaps()}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	old := testCaps()
	old.MaxFiles = 1
	old.CheckedAt = p.now().UTC().Format(time.RFC3339)
	p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.Capabilities = old })
	rec, _ := p.accounts.Record(acc.ID)

	png, _ := p.media.Put("ref", "image/png", ".png", []byte("png"))
	job := MuseJob{References: []string{png, png}}
	atts, err := p.attachments(context.Background(), job, rec, page)
	if err != nil || len(atts) != 2 || page.capsReads != 1 {
		t.Fatalf("atts = %d, err = %v, reads = %d", len(atts), err, page.capsReads)
	}

	old.CheckedAt = p.now().Add(-2 * capabilitiesTTL).UTC().Format(time.RFC3339)
	p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.Capabilities = old })
	rec, _ = p.accounts.Record(acc.ID)
	if caps, read, err := p.capabilities(context.Background(), rec, page, false); err != nil || !read || caps.MaxFiles != 2 {
		t.Fatalf("stale limits kept: %+v %v %v", caps, read, err)
	}
}
