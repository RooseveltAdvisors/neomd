package ui

// Rescheduling a queued send-later message (E on it → l with a new time, or
// enter to send now) must replace the original queued copy — but only AFTER
// the replacement is safely stored or sent, and never when the session ends
// any other way (abort/discard/error): then the original stays untouched.

import (
	"strings"
	"testing"
	"time"

	"github.com/sspaeti/neomd/internal/config"
	"github.com/sspaeti/neomd/internal/imap"
)

func requeueTestModel() Model {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{Name: "Personal", User: "me@example.com", From: "Me <me@example.com>"},
		},
		Folders: config.FoldersConfig{Scheduled: "Scheduled", Trash: "Trash", Sent: "Sent"},
	}
	return Model{cfg: cfg, accounts: cfg.ActiveAccounts(), compose: newComposeModel()}
}

// E on a queued message remembers it for replacement; E on a regular draft
// clears any stale requeue state.
func TestContinueDraftTracksQueuedOriginal(t *testing.T) {
	m := requeueTestModel()
	m.openEmail = &imap.Email{
		UID: 7, Folder: "Scheduled", Subject: "queued",
		SendAt: time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC),
	}
	mm, _ := m.continueDraft()
	m = mm.(Model)
	if m.requeue.uid != 7 || m.requeue.folder != "Scheduled" || m.requeue.account != "Personal" {
		t.Fatalf("requeue not tracked: %+v", m.requeue)
	}

	m.openEmail = &imap.Email{UID: 8, Folder: "Drafts", Subject: "regular draft"}
	mm, _ = m.continueDraft()
	m = mm.(Model)
	if m.requeue.uid != 0 {
		t.Fatalf("regular draft must clear requeue state: %+v", m.requeue)
	}
}

// Successful re-schedule fires the cleanup of the old copy and clears the
// tracking; a failed schedule keeps the old copy (no cleanup) — it is still
// the only valid version.
func TestScheduleDoneReplacesQueuedOriginal(t *testing.T) {
	m := requeueTestModel()
	m.requeue = requeueRef{uid: 7, folder: "Scheduled", account: "Personal"}

	mm, cmd := m.Update(scheduleDoneMsg{at: time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)})
	m = mm.(Model)
	if m.requeue.uid != 0 {
		t.Error("requeue not cleared after successful schedule")
	}
	if !strings.Contains(m.status, "Replaced the previous schedule") {
		t.Errorf("status = %q", m.status)
	}
	if cmd == nil {
		t.Fatal("no cleanup command fired")
	}
	if _, ok := cmd().(requeueCleanupDoneMsg); !ok {
		t.Fatal("cleanup command did not produce requeueCleanupDoneMsg")
	}

	// Failure path: old copy must survive (no cleanup command).
	m2 := requeueTestModel()
	m2.requeue = requeueRef{uid: 7, folder: "Scheduled", account: "Personal"}
	mm2, cmd2 := m2.Update(scheduleDoneMsg{err: errTest})
	m2 = mm2.(Model)
	if m2.requeue.uid != 0 {
		t.Error("requeue must be cleared on error too (stale state)")
	}
	if cmd2 != nil {
		t.Error("failed schedule must NOT delete the original queued copy")
	}
}

// Sending immediately (enter instead of l) also supersedes the queued copy.
func TestSendDoneReplacesQueuedOriginal(t *testing.T) {
	m := requeueTestModel()
	m.requeue = requeueRef{uid: 7, folder: "Scheduled", account: "Personal"}
	mm, cmd := m.Update(sendDoneMsg{})
	m = mm.(Model)
	if m.requeue.uid != 0 {
		t.Error("requeue not cleared after send")
	}
	if cmd == nil {
		t.Fatal("no cleanup command fired after send")
	}
	if _, ok := cmd().(requeueCleanupDoneMsg); !ok {
		t.Fatal("cleanup command did not produce requeueCleanupDoneMsg")
	}

	// Failed send: old queued copy stays.
	m2 := requeueTestModel()
	m2.requeue = requeueRef{uid: 7, folder: "Scheduled", account: "Personal"}
	_, cmd2 := m2.Update(sendDoneMsg{err: errTest})
	if cmd2 != nil {
		t.Error("failed send must NOT delete the original queued copy")
	}
}

// Aborting the editor keeps the original queued message untouched.
func TestEditorAbortKeepsQueuedOriginal(t *testing.T) {
	m := requeueTestModel()
	m.requeue = requeueRef{uid: 7, folder: "Scheduled", account: "Personal"}
	mm, cmd := m.Update(editorDoneMsg{aborted: true})
	m = mm.(Model)
	if m.requeue.uid != 0 {
		t.Error("requeue not cleared on abort")
	}
	if cmd != nil {
		t.Error("abort must NOT delete the original queued copy")
	}
}

// Cleanup failure must warn loudly — the daemon would deliver both copies.
func TestRequeueCleanupFailureWarns(t *testing.T) {
	m := requeueTestModel()
	mm, _ := m.Update(requeueCleanupDoneMsg{err: errTest})
	m = mm.(Model)
	if !m.isError || !strings.Contains(m.status, "OLD queued copy") {
		t.Errorf("cleanup failure not surfaced: %q", m.status)
	}
}

type testErr string

func (e testErr) Error() string { return string(e) }

var errTest = testErr("boom")
