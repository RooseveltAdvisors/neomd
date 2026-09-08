package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/imap"
)

// ── Universal undo journal ───────────────────────────────────────────────

// pressKeyMsg feeds a raw KeyMsg through the inbox handler.
func pressKeyMsg(m Model, msg tea.KeyMsg) Model {
	next, _ := m.updateInbox(msg)
	return next.(Model)
}

// TestUndoJournalScreenerMove pins the F/I/P fix: a screener move confirmed
// by the server lands in the undo journal, and u pops the most recent entry
// and issues a reverse-move command. The command is never executed here (it
// needs a live IMAP connection); the journal transition is the contract.
func TestUndoJournalScreenerMove(t *testing.T) {
	m := keysTestModel(t, 4)
	target := m.emails[0]

	after := press(m, "F") // feed move
	if len(after.optimistic) != 1 {
		t.Fatal("fixture: optimistic batch missing")
	}
	var id int
	for k := range after.optimistic {
		id = k
	}
	done, _ := after.Update(batchDoneMsg{optID: id, undo: []undoMove{{
		uid: 777, fromFolder: target.Folder, toFolder: "Feed",
	}}})
	confirmed := done.(Model)
	if len(confirmed.undoStack) != 1 {
		t.Fatalf("screener move not journaled: %d entries", len(confirmed.undoStack))
	}
	entry := confirmed.undoStack[0]
	if len(entry.moves) != 1 || entry.moves[0].toFolder != "Feed" || entry.moves[0].fromFolder != target.Folder {
		t.Fatalf("journal entry = %#v, want reverse move Feed → %s", entry, target.Folder)
	}
	if len(entry.seen) != 0 {
		t.Fatalf("move entry carries seen flags: %#v", entry.seen)
	}

	undone := pressKeyMsg(confirmed, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if len(undone.undoStack) != 0 {
		t.Fatalf("u did not pop the journal: %d entries left", len(undone.undoStack))
	}
	if !undone.loading {
		t.Fatal("undo should refetch the folder")
	}
}

// TestUndoJournalReadToggle pins un-read: the read/unread toggle journals the
// OLD state so u restores it exactly.
func TestUndoJournalReadToggle(t *testing.T) {
	m := keysTestModel(t, 2)
	m.emails[0].Seen = false

	toggled, _ := m.Update(toggleSeenDoneMsg{uid: m.emails[0].UID, folder: "INBOX", seen: true})
	after := toggled.(Model)
	if len(after.undoStack) != 1 || len(after.undoStack[0].seen) != 1 {
		t.Fatalf("toggle not journaled: %#v", after.undoStack)
	}
	flag := after.undoStack[0].seen[0]
	if flag.uid != m.emails[0].UID || flag.folder != "INBOX" || flag.markSeen {
		t.Fatalf("restore flag = %#v, want uid %d INBOX markSeen=false", flag, m.emails[0].UID)
	}

	undone := pressKeyMsg(after, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if len(undone.undoStack) != 0 {
		t.Fatal("u did not pop the journal")
	}
}

// TestUndoJournalMarkAllRead pins ctrl+n: marking a folder read journals a
// restore-to-unread entry for every email.
func TestUndoJournalMarkAllRead(t *testing.T) {
	m := keysTestModel(t, 3)
	m.emails[1].Seen = true // already read: not part of the action

	cmd := m.markAllSeenCmd()
	if cmd == nil {
		t.Fatal("markAllSeenCmd returned nil")
	}
	// Simulate the server confirming: the message carries restore flags.
	flags := []seenFlag{
		{folder: "INBOX", uid: m.emails[0].UID, markSeen: false},
		{folder: "INBOX", uid: m.emails[2].UID, markSeen: false},
	}
	done, _ := m.Update(batchDoneMsg{seen: flags})
	after := done.(Model)
	if len(after.undoStack) != 1 || len(after.undoStack[0].seen) != 2 {
		t.Fatalf("mark-all-read not journaled: %#v", after.undoStack)
	}
	for _, f := range after.undoStack[0].seen {
		if f.markSeen {
			t.Fatalf("restore flag marks seen: %#v", f)
		}
	}
}

// TestUndoJournalMostRecentFirst pins LIFO order: u reverses the most recent
// action, then the one before it.
func TestUndoJournalMostRecentFirst(t *testing.T) {
	m := keysTestModel(t, 4)

	first, _ := m.Update(batchDoneMsg{undo: []undoMove{{uid: 1, fromFolder: "INBOX", toFolder: "Archive"}}})
	second, _ := first.(Model).Update(batchDoneMsg{undo: []undoMove{{uid: 2, fromFolder: "INBOX", toFolder: "Feed"}}})
	third, _ := second.(Model).Update(batchDoneMsg{seen: []seenFlag{{folder: "INBOX", uid: 3, markSeen: false}}})
	full := third.(Model)
	if len(full.undoStack) != 3 {
		t.Fatalf("journal = %d entries, want 3", len(full.undoStack))
	}

	pop1 := pressKeyMsg(full, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if len(pop1.undoStack) != 2 || len(pop1.undoStack[1].seen) != 0 {
		t.Fatalf("first undo took the wrong entry: %#v", pop1.undoStack)
	}
	pop2 := pressKeyMsg(pop1, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if len(pop2.undoStack) != 1 || pop2.undoStack[0].moves[0].toFolder != "Archive" {
		t.Fatalf("second undo took the wrong entry: %#v", pop2.undoStack)
	}
}

// TestUndoJournalCap pins maxUndoStack: the journal stays bounded.
func TestUndoJournalCap(t *testing.T) {
	m := keysTestModel(t, 2)
	for i := 0; i < maxUndoStack+5; i++ {
		next, _ := m.Update(batchDoneMsg{undo: []undoMove{{uid: uint32(i + 1), fromFolder: "INBOX", toFolder: "Archive"}}})
		m = next.(Model)
	}
	if len(m.undoStack) != maxUndoStack {
		t.Fatalf("journal = %d entries, want cap %d", len(m.undoStack), maxUndoStack)
	}
}

// TestUndoNothingToDo pins the visible status when there is nothing to undo.
func TestUndoNothingToDo(t *testing.T) {
	m := keysTestModel(t, 2)
	after := pressKeyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if after.status != "Nothing to undo." {
		t.Fatalf("status = %q", after.status)
	}
}

// TestUndoRefusesInReadOnly pins that u never touches the network in
// read-only profiles; the returned command reports the refusal.
func TestUndoRefusesInReadOnly(t *testing.T) {
	m := keysTestModel(t, 2)
	m.cfg.ReadOnly = true
	next, _ := m.Update(batchDoneMsg{undo: []undoMove{{uid: 1, fromFolder: "INBOX", toFolder: "Archive"}}})
	withEntry := next.(Model)
	undone := pressKeyMsg(withEntry, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	// The command must report read-only before dialing — execute it.
	cmd := undone.undoActionCmd(undoAction{moves: []undoMove{{uid: 1, fromFolder: "INBOX", toFolder: "Archive"}}})
	if cmd == nil {
		t.Fatal("undo returned no command")
	}
	msg := cmd()
	bd, ok := msg.(batchDoneMsg)
	if !ok || bd.err == nil || bd.err.Error() != "neomd read-only mode: undo blocked" {
		t.Fatalf("undo cmd result = %#v, want read-only batch error", msg)
	}
}

// ── Quick peek ───────────────────────────────────────────────────────────

// leaderPeek presses space twice (leader + space) and returns the result.
func leaderPeek(m Model) Model {
	m = pressKeyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	return pressKeyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
}

// TestPeekOpensAndClosesWithStableSelection pins the leader+space toggle: it
// opens a preview of the highlighted email without leaving the list, and
// closing (esc or the chord again) keeps the cursor exactly where it was.
func TestPeekOpensAndClosesWithStableSelection(t *testing.T) {
	m := keysTestModel(t, 4)
	m.inbox.Select(2)
	want := m.inbox.Index()

	opened := leaderPeek(m)
	if !opened.peekActive {
		t.Fatal("leader+space did not open the peek pane")
	}
	if opened.peekEmail == nil || opened.peekEmail.UID != m.emails[2].UID {
		t.Fatalf("peek targets %#v, want the highlighted email", opened.peekEmail)
	}
	if opened.state != stateInbox {
		t.Fatalf("peek left the inbox: state=%v", opened.state)
	}
	if opened.inbox.Index() != want {
		t.Fatalf("cursor moved on open: %d, want %d", opened.inbox.Index(), want)
	}

	// esc closes; the chord toggles too. Both keep the selection.
	closed := pressKeyMsg(opened, tea.KeyMsg{Type: tea.KeyEsc})
	if closed.peekActive || closed.peekEmail != nil {
		t.Fatal("esc did not close the peek pane")
	}
	if closed.inbox.Index() != want {
		t.Fatalf("cursor moved on close: %d, want %d", closed.inbox.Index(), want)
	}
	toggled := leaderPeek(closed)
	if !toggled.peekActive {
		t.Fatal("leader+space did not reopen the peek pane")
	}
	toggledClosed := leaderPeek(toggled)
	if toggledClosed.peekActive {
		t.Fatal("leader+space did not close the peek pane")
	}
	if toggledClosed.inbox.Index() != want {
		t.Fatalf("cursor moved after toggle close: %d, want %d", toggledClosed.inbox.Index(), want)
	}
}

// TestPeekStaleResultDropped pins that a body fetch racing the cursor (or the
// pane closing) never fills the pane with the wrong email.
func TestPeekStaleResultDropped(t *testing.T) {
	m := keysTestModel(t, 3)
	m.inbox.Select(2)
	opened := leaderPeek(m)

	// Wrong email arrives: ignored.
	wrong := &imap.Email{UID: 999, Folder: "INBOX"}
	next, _ := opened.Update(peekLoadedMsg{email: wrong, body: "wrong body"})
	if got := next.(Model); got.peekBody != "" {
		t.Fatal("stale peek body was applied")
	}

	// Right email arrives: applied.
	right := &imap.Email{UID: m.emails[2].UID, Folder: "INBOX"}
	next, _ = opened.Update(peekLoadedMsg{email: right, body: "hello peek"})
	if got := next.(Model); got.peekBody != "hello peek" {
		t.Fatalf("peek body = %q", got.peekBody)
	}

	// Closed pane: ignored even for the right email.
	shut := pressKeyMsg(opened, tea.KeyMsg{Type: tea.KeyEsc})
	next, _ = shut.Update(peekLoadedMsg{email: right, body: "late"})
	if got := next.(Model); got.peekBody != "" {
		t.Fatal("peek body applied after close")
	}
}

// TestPeekResizesList pins that opening the peek shrinks the list by exactly
// the pane height and closing restores it, so the status bar never overflows.
func TestPeekResizesList(t *testing.T) {
	m := keysTestModel(t, 4)
	m.height = 60
	m.inbox.SetSize(80, 56) // WindowSizeMsg layout: 60-4
	full := m.inbox.Height()

	opened := leaderPeek(m)
	shrunk := opened.inbox.Height()
	if shrunk != full-opened.peekPaneHeight() {
		t.Fatalf("peek list height = %d, want %d", shrunk, full-opened.peekPaneHeight())
	}

	closed := pressKeyMsg(opened, tea.KeyMsg{Type: tea.KeyEsc})
	if closed.inbox.Height() != full {
		t.Fatalf("closed list height = %d, want %d", closed.inbox.Height(), full)
	}
}
