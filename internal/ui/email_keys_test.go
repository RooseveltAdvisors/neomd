package ui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/config"
	"github.com/sspaeti/neomd/internal/imap"
	"github.com/sspaeti/neomd/internal/screener"
	"github.com/sspaeti/neomd/internal/snippets"
)

// ── Fixtures ─────────────────────────────────────────────────────────────

func keysTestConfig() *config.Config {
	return &config.Config{
		Folders: config.FoldersConfig{
			Inbox: "INBOX", Archive: "Archive", Trash: "Trash", Waiting: "Waiting",
			ScreenedOut: "ScreenedOut", Feed: "Feed", PaperTrail: "PaperTrail",
			ToScreen: "ToScreen", Work: "Work", Sent: "Sent", Drafts: "Drafts",
			Someday: "Someday", Scheduled: "Scheduled", Spam: "Spam",
		},
		UI: config.UIConfig{InboxCount: 3},
	}
}

func keysTestEmails(n int) []imap.Email {
	out := make([]imap.Email, n)
	base := time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = imap.Email{
			UID:     uint32(100 - i), // newest first: descending UIDs
			Folder:  "INBOX",
			From:    "sender@example.com",
			Subject: "message",
			Date:    base.Add(-time.Duration(i) * time.Hour),
		}
	}
	return out
}

// keysTestModel returns an inbox-state model holding n emails in INBOX.
func keysTestModel(t *testing.T, n int) Model {
	t.Helper()
	m := Model{
		state:      stateInbox,
		inbox:      newInboxList(80, 20, "Sent", "Drafts"),
		folders:    []string{"Inbox"},
		cfg:        keysTestConfig(),
		emails:     keysTestEmails(n),
		markedUIDs: map[uint32]bool{},
		compose:    newComposeModel(),
		screener:   &screener.Screener{},
	}
	m.sortField, m.sortReverse = "date", true
	m.applyFilter()
	if len(m.inbox.Items()) != n {
		t.Fatalf("fixture: list has %d items, want %d", len(m.inbox.Items()), n)
	}
	return m
}

func press(m Model, key string) Model {
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.updateInbox(msg)
	return next.(Model)
}

// ── 1. Binding map ───────────────────────────────────────────────────────

// TestEmailBindingMap pins the keyboard contract: the four core keys the
// client is driven by (lowercase by contract), plus the uppercase mailbox
// action keys (I/O/P/B/F) and the aliases that had to stay.
func TestEmailBindingMap(t *testing.T) {
	tests := []struct {
		key   string
		check func(t *testing.T, before, after Model)
	}{
		{"e", func(t *testing.T, before, after Model) { // archive
			if len(after.emails) != len(before.emails)-1 {
				t.Fatalf("e: %d emails left, want %d", len(after.emails), len(before.emails)-1)
			}
		}},
		{"A", func(t *testing.T, before, after Model) { // archive, legacy alias
			if len(after.emails) != len(before.emails)-1 {
				t.Fatalf("A alias: %d emails left, want %d", len(after.emails), len(before.emails)-1)
			}
		}},
		{"h", func(t *testing.T, _, after Model) { // remind
			if !after.reminderActive || len(after.reminderTargets) != 1 {
				t.Fatalf("h: reminderActive=%v targets=%d", after.reminderActive, len(after.reminderTargets))
			}
		}},
		{"s", func(t *testing.T, _, after Model) { // start an email
			if after.state != stateCompose {
				t.Fatalf("s: state = %v, want stateCompose", after.state)
			}
		}},
		{"c", func(t *testing.T, _, after Model) { // compose, legacy alias
			if after.state != stateCompose {
				t.Fatalf("c alias: state = %v, want stateCompose", after.state)
			}
		}},
		{";", func(t *testing.T, _, after Model) { // snippets
			if after.state != stateSnippets {
				t.Fatalf("; : state = %v, want stateSnippets", after.state)
			}
		}},
		{"x", func(t *testing.T, before, after Model) { // select
			if len(after.emails) != len(before.emails) || !after.markedUIDs[before.emails[0].UID] {
				t.Fatalf("x: selection changed unexpectedly: emails=%d selected=%v", len(after.emails), after.markedUIDs)
			}
		}},
		{"#", func(t *testing.T, before, after Model) { // trash
			if len(after.emails) != len(before.emails)-1 {
				t.Fatalf("#: %d emails left, want %d", len(after.emails), len(before.emails)-1)
			}
		}},
		{"I", func(t *testing.T, before, after Model) { // screen in (mailbox actions are uppercase)
			if len(after.emails) != len(before.emails)-1 {
				t.Fatalf("I: %d emails left, want %d", len(after.emails), len(before.emails)-1)
			}
		}},
		{"O", func(t *testing.T, before, after Model) { // screen out
			if len(after.emails) != len(before.emails)-1 {
				t.Fatalf("O: %d emails left, want %d", len(after.emails), len(before.emails)-1)
			}
		}},
		{"P", func(t *testing.T, before, after Model) { // papertrail
			if len(after.emails) != len(before.emails)-1 {
				t.Fatalf("P: %d emails left, want %d", len(after.emails), len(before.emails)-1)
			}
		}},
		{"B", func(t *testing.T, before, after Model) { // work
			if len(after.emails) != len(before.emails)-1 {
				t.Fatalf("B: %d emails left, want %d", len(after.emails), len(before.emails)-1)
			}
		}},
		{"F", func(t *testing.T, before, after Model) { // feed stays uppercase: f is forward
			if len(after.emails) != len(before.emails)-1 {
				t.Fatalf("F: %d emails left, want %d", len(after.emails), len(before.emails)-1)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			before := keysTestModel(t, 4)
			tt.check(t, before, press(before, tt.key))
		})
	}
}

// TestLowercaseMailboxActionsAreDead pins that the former lowercase action
// keys (i, p, b) no longer fire anything in the inbox - mailbox actions are
// uppercase now, and the letters stay free for future use.
func TestLowercaseMailboxActionsAreDead(t *testing.T) {
	for _, key := range []string{"i", "p", "b"} {
		t.Run(key, func(t *testing.T) {
			before := keysTestModel(t, 4)
			after := press(before, key)
			if len(after.emails) != 4 || len(after.optimistic) != 0 || after.reminderActive || after.state != stateInbox {
				t.Fatalf("lowercase %q still fires an action: emails=%d opt=%d state=%v", key, len(after.emails), len(after.optimistic), after.state)
			}
		})
	}
}

// TestEmailBindingsGuardedInsideInputFields pins spec: none of the new keys
// fire while a text field owns the keyboard — they must land as characters.
func TestEmailBindingsGuardedInsideInputFields(t *testing.T) {
	for _, key := range []string{"e", "h", "s", ";"} {
		t.Run("filter/"+key, func(t *testing.T) {
			m := keysTestModel(t, 4)
			m.filterActive = true
			after := press(m, key)
			if len(after.emails) != 4 || after.state != stateInbox || after.reminderActive {
				t.Fatalf("%q fired inside the filter field", key)
			}
			if after.filterText != key {
				t.Fatalf("filterText = %q, want %q — key was swallowed", after.filterText, key)
			}
		})
		t.Run("cmdline/"+key, func(t *testing.T) {
			m := keysTestModel(t, 4)
			m.cmdMode = true
			after := press(m, key)
			if len(after.emails) != 4 || after.state != stateInbox || after.reminderActive {
				t.Fatalf("%q fired inside the : command line", key)
			}
		})
		t.Run("reminder-prompt/"+key, func(t *testing.T) {
			m := keysTestModel(t, 4)
			m.reminderActive = true
			m.reminderInput = newReminderInput()
			after := press(m, key)
			if len(after.emails) != 4 || after.state != stateInbox {
				t.Fatalf("%q fired inside the reminder prompt", key)
			}
		})
	}
}

// TestReaderArchiveAndRemindKeys pins the same keys in the reader, including
// the h → remind move (back-to-inbox stays on q/esc).
func TestReaderArchiveAndRemindKeys(t *testing.T) {
	open := imap.Email{UID: 100, Folder: "INBOX", From: "a@b.com"}

	m := keysTestModel(t, 4)
	m.state = stateReading
	m.openEmail = &open
	after, _ := m.updateReader(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := after.(Model)
	if got.state != stateInbox || len(got.emails) != 3 {
		t.Fatalf("reader e: state=%v emails=%d, want inbox/3", got.state, len(got.emails))
	}

	m2 := keysTestModel(t, 4)
	m2.state = stateReading
	m2.openEmail = &open
	after2, _ := m2.updateReader(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	got2 := after2.(Model)
	if !got2.reminderActive || got2.state != stateReading {
		t.Fatalf("reader h: reminderActive=%v state=%v, want prompt in reader", got2.reminderActive, got2.state)
	}

	// q still leaves the reader — h no longer does.
	m3 := keysTestModel(t, 4)
	m3.state = stateReading
	m3.openEmail = &open
	after3, _ := m3.updateReader(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if after3.(Model).state != stateInbox {
		t.Fatal("reader q should return to the inbox")
	}
}

// ── 2. Optimistic update path ────────────────────────────────────────────

// TestOptimisticArchiveIsInstant pins that the row leaves the list on the
// keystroke — before any IMAP round-trip — and that no folder re-fetch is
// queued when the server later confirms.
func TestOptimisticArchiveIsInstant(t *testing.T) {
	m := keysTestModel(t, 4)
	target := m.emails[0]

	after := press(m, "e")
	if len(after.emails) != 3 || len(after.inbox.Items()) != 3 {
		t.Fatalf("archive not applied optimistically: emails=%d items=%d", len(after.emails), len(after.inbox.Items()))
	}
	for _, e := range after.emails {
		if e.UID == target.UID {
			t.Fatal("archived email is still in the list")
		}
	}
	if len(after.optimistic) != 1 {
		t.Fatalf("optimistic batches = %d, want 1 pending rollback snapshot", len(after.optimistic))
	}

	// Server confirms: the snapshot retires and the list is left alone.
	var id int
	for k := range after.optimistic {
		id = k
	}
	done, _ := after.Update(batchDoneMsg{optID: id})
	confirmed := done.(Model)
	if len(confirmed.emails) != 3 {
		t.Fatalf("confirm changed the list: %d emails, want 3", len(confirmed.emails))
	}
	if len(confirmed.optimistic) != 0 {
		t.Fatal("confirmed batch should retire its rollback snapshot")
	}
	if confirmed.loading {
		t.Fatal("confirming an optimistic batch must not put the UI back into loading")
	}
}

// TestOptimisticArchiveRollsBackVisiblyOnFailure pins that a server refusal
// puts the row back and says so, rather than silently dropping the action.
func TestOptimisticArchiveRollsBackVisiblyOnFailure(t *testing.T) {
	m := keysTestModel(t, 4)
	target := m.emails[0]
	after := press(m, "e")

	var id int
	for k := range after.optimistic {
		id = k
	}
	done, _ := after.Update(batchDoneMsg{optID: id, err: errors.New("IMAP said no")})
	rolled := done.(Model)

	if len(rolled.emails) != 4 || len(rolled.inbox.Items()) != 4 {
		t.Fatalf("rollback: emails=%d items=%d, want 4/4", len(rolled.emails), len(rolled.inbox.Items()))
	}
	var found bool
	for _, e := range rolled.emails {
		if e.UID == target.UID {
			found = true
		}
	}
	if !found {
		t.Fatal("rolled-back email did not return to the list")
	}
	if !rolled.isError || rolled.status == "" {
		t.Fatalf("rollback must be visible: isError=%v status=%q", rolled.isError, rolled.status)
	}
	if len(rolled.optimistic) != 0 {
		t.Fatal("rolled-back batch should release its snapshot")
	}
}

// TestOverlappingOptimisticBatchesRollBackIndependently pins that firing two
// actions before either acks does not let one ack clobber the other's undo.
func TestOverlappingOptimisticBatchesRollBackIndependently(t *testing.T) {
	m := keysTestModel(t, 4)
	first := press(m, "e")                  // archive newest
	second := press(press(first, "d"), "d") // dd deletes the next one
	if len(second.emails) != 2 || len(second.optimistic) != 2 {
		t.Fatalf("two pending batches: emails=%d snapshots=%d", len(second.emails), len(second.optimistic))
	}

	var ids []int
	for k := range second.optimistic {
		ids = append(ids, k)
	}
	// Confirm one, fail the other: exactly one row must come back.
	ok, _ := second.Update(batchDoneMsg{optID: ids[0]})
	next := ok.(Model)
	failed, _ := next.Update(batchDoneMsg{optID: ids[1], err: errors.New("nope")})
	end := failed.(Model)
	if len(end.emails) != 3 {
		t.Fatalf("after one ack + one failure: %d emails, want 3", len(end.emails))
	}
	if len(end.optimistic) != 0 {
		t.Fatalf("both batches should be released, %d left", len(end.optimistic))
	}
}

// TestOptimisticRemindMovesRowsAtOnce pins the reminder path through the same
// optimistic machinery as archive.
func TestOptimisticRemindMovesRowsAtOnce(t *testing.T) {
	m := keysTestModel(t, 4)
	m = press(m, "h")
	if !m.reminderActive {
		t.Fatal("h did not open the reminder prompt")
	}
	m.reminderInput.SetValue("+2h")
	next, _ := m.updateInbox(tea.KeyMsg{Type: tea.KeyEnter})
	after := next.(Model)
	if after.reminderActive {
		t.Fatal("prompt should close on enter")
	}
	if len(after.emails) != 3 || len(after.optimistic) != 1 {
		t.Fatalf("remind: emails=%d snapshots=%d, want 3/1", len(after.emails), len(after.optimistic))
	}
}

// ── 3. Infinite scroll ───────────────────────────────────────────────────

func TestOldestUIDIsThePagingCursor(t *testing.T) {
	emails := []imap.Email{
		{UID: 40, Folder: "INBOX"}, {UID: 12, Folder: "INBOX"},
		{UID: 7, Folder: "Archive"}, // other folder must not lower the cursor
	}
	if got := oldestUID(emails, "INBOX"); got != 12 {
		t.Fatalf("oldestUID = %d, want 12", got)
	}
	if got := oldestUID(emails, "Feed"); got != 0 {
		t.Fatalf("oldestUID for an unheld folder = %d, want 0", got)
	}
}

func TestAppendNewEmailsDeduplicates(t *testing.T) {
	have := []imap.Email{{UID: 9, Folder: "INBOX"}}
	got, added := appendNewEmails(have, []imap.Email{
		{UID: 9, Folder: "INBOX"},   // already held
		{UID: 9, Folder: "Archive"}, // same UID, different folder → new
		{UID: 8, Folder: "INBOX"},
	})
	if added != 2 || len(got) != 3 {
		t.Fatalf("added=%d len=%d, want 2/3", added, len(got))
	}
	if _, again := appendNewEmails(got, got); again != 0 {
		t.Fatalf("re-appending the same page added %d", again)
	}
}

// TestScrollToBottomTriggersNextPage pins that reaching the end of the list
// fetches more — and that the middle of the list does not.
func TestScrollToBottomTriggersNextPage(t *testing.T) {
	m := keysTestModel(t, 12)

	m.inbox.Select(0)
	if cmd := m.maybeLoadMoreCmd(); cmd != nil {
		t.Fatal("top of the list should not page")
	}

	m.inbox.Select(len(m.inbox.Items()) - 1)
	if cmd := m.maybeLoadMoreCmd(); cmd == nil {
		t.Fatal("bottom of the list should page")
	}
	if !m.loadingMore {
		t.Fatal("paging should latch loadingMore so the fetch is not duplicated")
	}
	if cmd := m.maybeLoadMoreCmd(); cmd != nil {
		t.Fatal("a second fetch fired while one was in flight")
	}

	m.loadingMore = false
	m.moreExhausted = true
	if cmd := m.maybeLoadMoreCmd(); cmd != nil {
		t.Fatal("exhausted folder should not keep fetching")
	}
}

func TestScrollDoesNotPageAdHocViews(t *testing.T) {
	m := keysTestModel(t, 12)
	m.inbox.Select(len(m.inbox.Items()) - 1)
	m.imapSearchResults = true
	if cmd := m.maybeLoadMoreCmd(); cmd != nil {
		t.Fatal("IMAP search results are not a paged folder view")
	}
	m.imapSearchResults = false
	m.offTabFolder = "Everything"
	if cmd := m.maybeLoadMoreCmd(); cmd != nil {
		t.Fatal("cross-folder views are not paged")
	}
}

// TestNextPageAppendsAndKeepsScrollPosition pins the append: rows are added,
// nothing is replaced, and the cursor stays on the row the user was reading.
func TestNextPageAppendsAndKeepsScrollPosition(t *testing.T) {
	m := keysTestModel(t, 6)
	m.inbox.Select(4)
	at := m.inbox.Index()

	older := []imap.Email{
		{UID: 90, Folder: "INBOX", From: "old@example.com", Subject: "older", Date: time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)},
		{UID: 89, Folder: "INBOX", From: "old@example.com", Subject: "older", Date: time.Date(2026, time.February, 1, 8, 0, 0, 0, time.UTC)},
	}
	next, _ := m.Update(moreEmailsLoadedMsg{emails: older, folder: "INBOX"})
	after := next.(Model)

	if len(after.emails) != 8 || len(after.inbox.Items()) != 8 {
		t.Fatalf("append: emails=%d items=%d, want 8/8", len(after.emails), len(after.inbox.Items()))
	}
	if after.inbox.Index() != at {
		t.Fatalf("cursor moved on append: %d, want %d", after.inbox.Index(), at)
	}
	if after.loadingMore {
		t.Fatal("loadingMore should clear once the page lands")
	}
	if after.moreExhausted {
		t.Fatal("a page that added rows must not mark the folder exhausted")
	}
}

func TestEmptyNextPageMarksFolderExhausted(t *testing.T) {
	m := keysTestModel(t, 6)
	next, _ := m.Update(moreEmailsLoadedMsg{emails: nil, folder: "INBOX"})
	after := next.(Model)
	if !after.moreExhausted || len(after.emails) != 6 {
		t.Fatalf("exhausted=%v emails=%d, want true/6", after.moreExhausted, len(after.emails))
	}
}

func TestNextPageForAnotherFolderIsDropped(t *testing.T) {
	m := keysTestModel(t, 6)
	next, _ := m.Update(moreEmailsLoadedMsg{
		emails: []imap.Email{{UID: 5, Folder: "Archive"}},
		folder: "Archive", // user switched tabs while the page was in flight
	})
	if got := next.(Model); len(got.emails) != 6 {
		t.Fatalf("stale page merged into the wrong folder: %d emails", len(got.emails))
	}
}

func TestFullFolderLoadResetsPagingState(t *testing.T) {
	m := keysTestModel(t, 6)
	m.moreExhausted = true
	m.loadingMore = true
	m.optimistic = map[int][]imap.Email{1: {{UID: 1}}}
	next, _ := m.Update(emailsLoadedMsg{emails: keysTestEmails(2), folder: "INBOX"})
	after := next.(Model)
	if after.moreExhausted || after.loadingMore || after.optimistic != nil {
		t.Fatalf("reload should reset paging/optimistic state: exhausted=%v more=%v opt=%v",
			after.moreExhausted, after.loadingMore, after.optimistic)
	}
}

// ── Snippets ─────────────────────────────────────────────────────────────

func TestSnippetParseSplitsSubjectAndBody(t *testing.T) {
	got := snippets.Parse("intro", "Subject: Quick intro\n\nHi there,\n\nBody line.\n")
	if got.Subject != "Quick intro" {
		t.Fatalf("subject = %q", got.Subject)
	}
	if got.Body != "Hi there,\n\nBody line.\n" {
		t.Fatalf("body = %q", got.Body)
	}

	plain := snippets.Parse("plain", "No header here.\nSecond line.\n")
	if plain.Subject != "" || plain.Body != "No header here.\nSecond line.\n" {
		t.Fatalf("headerless snippet mis-parsed: %+v", plain)
	}
}

func TestSnippetPickerComposesPrefilled(t *testing.T) {
	m := keysTestModel(t, 2)
	m.snippets = []snippets.Snippet{{Name: "intro", Subject: "Quick intro", Body: "Hi there,\n"}}
	m.state = stateSnippets
	next, _ := m.updateSnippets(tea.KeyMsg{Type: tea.KeyEnter})
	after := next.(Model)
	if after.state != stateCompose {
		t.Fatalf("state = %v, want stateCompose", after.state)
	}
	if after.compose.subject.Value() != "Quick intro" {
		t.Fatalf("subject = %q", after.compose.subject.Value())
	}
	if after.mailtoBody != "Hi there,\n" {
		t.Fatalf("body not staged for the editor: %q", after.mailtoBody)
	}
}

// TestScrollLatchSurvivesTheKeyHandler pins that the loadingMore latch set by
// maybeLoadMoreCmd is present on the model the handler RETURNS — a plain
// `return m, m.maybeLoadMoreCmd()` would drop it and re-fetch on every keypress.
func TestScrollLatchSurvivesTheKeyHandler(t *testing.T) {
	for _, key := range []string{"G", "j", "d"} {
		t.Run(key, func(t *testing.T) {
			m := keysTestModel(t, 12)
			m.inbox.Select(len(m.inbox.Items()) - 1)
			after := press(m, key)
			if !after.loadingMore {
				t.Fatalf("%q at the bottom did not latch loadingMore on the returned model", key)
			}
		})
	}
}
