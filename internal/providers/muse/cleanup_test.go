package muse

import (
	"context"
	"testing"
)

// "Dọn đoạn chat phụ" sends the cleanup message in a new chat and returns
// Muse's reply.
func TestCleanChatsSendsTheCleanupMessage(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "clean"}, statuses: []string{"completed"},
		events: []AssistantEvent{{Text: "Đã xoá 42 đoạn chat."}}}
	p := testProvider(t, page)
	addSignedIn(t, p, "A")

	res := p.CleanChats(context.Background())
	if !res.Success || len(res.Data) != 1 || res.Data[0].Error != "" || res.Data[0].Reply != "Đã xoá 42 đoạn chat." {
		t.Fatalf("result = %+v", res)
	}
	if want := p.cfg.Get().Muse.CleanupMessage; len(page.sends) != 1 || page.sends[0] != want {
		t.Fatalf("sends = %q, want %q", page.sends, want)
	}
	if p.cleaning[res.Data[0].AccountID] {
		t.Fatal("account still held after cleanup")
	}
}

// A job already sent is only checked in its own chat: deleting that chat
// would lose it, so its account is skipped and nothing is sent there.
func TestCleanChatsSkipsAnAccountWithAJobInProgress(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running"}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	if _, err := p.updateJob(job.ID, func(j *MuseJob) {
		j.Status, j.AccountID, j.SessionID = MuseJobProcessing, acc.ID, "s1"
	}); err != nil {
		t.Fatal(err)
	}

	res := p.CleanChats(context.Background())
	if !res.Success || len(res.Data) != 1 || res.Data[0].Error != errCleanBusy.Error() {
		t.Fatalf("result = %+v", res)
	}
	if len(page.sends) != 0 {
		t.Fatalf("sent %q on a busy account", page.sends)
	}
}

// An account being cleaned takes no job until the cleanup ends.
func TestCleaningAccountTakesNoJob(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	acc := addSignedIn(t, p, "A")
	if !p.reserveForClean(acc.ID) {
		t.Fatal("idle account not reserved")
	}
	if p.reserveForClean(acc.ID) {
		t.Fatal("account reserved twice")
	}
	p.endClean(acc.ID)
	if !p.reserveForClean(acc.ID) {
		t.Fatal("account not free after cleanup")
	}
}
