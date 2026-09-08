package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/contacts"
	"github.com/sspaeti/neomd/internal/imap"
	"github.com/sspaeti/neomd/internal/reminder"
	"github.com/sspaeti/neomd/internal/screener"
	"github.com/sspaeti/neomd/internal/snippets"
)

// ── Fixtures ─────────────────────────────────────────────────────────────

func uxTestModel(t *testing.T, n int) Model {
	t.Helper()
	return keysTestModel(t, n)
}

// pressLeader presses leader + key (two key messages).
func pressLeader(m Model, key string) Model {
	m = pressKeyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	return pressKeyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

// threadedUXEmails builds a small conversation: one root + two replies
// (In-ReplyTo chained), plus standalone emails. Newest first after sorting.
func threadedUXEmails() []imap.Email {
	base := time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC)
	root := imap.Email{
		UID: 100, Folder: "INBOX", From: "alice@example.com",
		Subject: "Project kickoff", Date: base,
		MessageID: "<root@example.com>",
	}
	reply1 := imap.Email{
		UID: 99, Folder: "INBOX", From: "bob@example.com",
		Subject: "Re: Project kickoff", Date: base.Add(time.Hour),
		MessageID: "<r1@example.com>", InReplyTo: "<root@example.com>",
		Seen: true,
	}
	reply2 := imap.Email{
		UID: 98, Folder: "INBOX", From: "alice@example.com",
		Subject: "Re: Project kickoff", Date: base.Add(2 * time.Hour),
		MessageID: "<r2@example.com>", InReplyTo: "<r1@example.com>",
	}
	other1 := imap.Email{
		UID: 97, Folder: "INBOX", From: "carol@example.com",
		Subject: "Lunch?", Date: base.Add(3 * time.Hour),
	}
	other2 := imap.Email{
		UID: 96, Folder: "INBOX", From: "dave@example.com",
		Subject: "Invoice attached", Date: base.Add(4 * time.Hour),
		HasAttachment: true, Seen: true,
	}
	return []imap.Email{root, reply1, reply2, other1, other2}
}

func threadedUXModel(t *testing.T) Model {
	t.Helper()
	m := keysTestModel(t, 0)
	m.emails = threadedUXEmails()
	m.sortField, m.sortReverse = "date", true
	m.applyFilter()
	return m
}

// ── 1. Fuzzy command palette ─────────────────────────────────────────────

func TestFuzzyScore(t *testing.T) {
	if _, ok := fuzzyScore("mark-read", "mkrd"); !ok {
		t.Error("mkrd should be a subsequence of mark-read")
	}
	if _, ok := fuzzyScore("mark-read", "zx"); ok {
		t.Error("zx should not be a subsequence of mark-read")
	}
	// Contiguous hits score better than scattered ones.
	contig, _ := fuzzyScore("mark-read", "mark")
	scatter, _ := fuzzyScore("mark-read", "mrd")
	if contig >= scatter {
		t.Errorf("contiguous score %d should beat scattered %d", contig, scatter)
	}
}

func TestMatchCmdsFuzzyFallback(t *testing.T) {
	// Prefix matches still win exclusively (existing cmdline tests rely on
	// this), so "sc" must not be polluted by fuzzy hits.
	for _, c := range matchCmdsFuzzy("sc") {
		if !strings.HasPrefix(c.name, "sc") && !hasAliasPrefix(c, "sc") {
			t.Errorf("prefix query %q matched non-prefix command %q", "sc", c.name)
		}
	}
	// Fuzzy kicks in only when nothing matches by prefix.
	got := matchCmdsFuzzy("mkrd")
	if len(got) == 0 || got[0].name != "mark-read" {
		t.Fatalf("fuzzy mkrd = %v, want mark-read first", cmdNames(got))
	}
	if got := matchCmdsFuzzy("zzzz"); len(got) != 0 {
		t.Fatalf("fuzzy zzzz matched %v, want none", cmdNames(got))
	}
}

func hasAliasPrefix(c *neomdCmd, prefix string) bool {
	for _, a := range c.aliases {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

func cmdNames(cmds []*neomdCmd) []string {
	var out []string
	for _, c := range cmds {
		out = append(out, c.name)
	}
	return out
}

func TestMatchCmdLineSplitsArgument(t *testing.T) {
	cmd, arg := matchCmdLine("move Work")
	if cmd == nil || cmd.name != "move" || arg != "Work" {
		t.Fatalf("matchCmdLine(\"move Work\") = %v, %q", cmd, arg)
	}
	cmd, arg = matchCmdLine("remind tomorrow 9am")
	if cmd == nil || cmd.name != "remind" || arg != "tomorrow 9am" {
		t.Fatalf("matchCmdLine(\"remind tomorrow 9am\") = %v, %q", cmd, arg)
	}
	cmd, _ = matchCmdLine("done")
	if cmd == nil || cmd.name != "done" {
		t.Fatalf("matchCmdLine(\"done\") = %v", cmd)
	}
	if cmd, _ := matchCmdLine(""); cmd != nil {
		t.Fatal("empty input should resolve to no command")
	}
}

func TestCmdLinePreviewDescribesEffect(t *testing.T) {
	m := uxTestModel(t, 2)
	got := m.cmdLinePreview("move Work")
	if !strings.Contains(got, "Work") || !strings.Contains(got, "1 email") {
		t.Errorf("preview move Work = %q, want folder and target count", got)
	}
	got = m.cmdLinePreview("move nope")
	if !strings.Contains(got, "unknown folder") {
		t.Errorf("preview move nope = %q, want unknown-folder warning", got)
	}
	got = m.cmdLinePreview("done")
	if !strings.Contains(got, "Archive") {
		t.Errorf("preview done = %q, want archive destination", got)
	}
	if got = m.cmdLinePreview("remind nonsense time"); !strings.Contains(got, "invalid time") {
		t.Errorf("preview remind nonsense = %q, want invalid-time warning", got)
	}
	if got = m.cmdLinePreview("zzz"); got != "" {
		t.Errorf("preview for unknown command = %q, want empty", got)
	}
}

func TestCmdDoneArchivesViaOptimisticPath(t *testing.T) {
	m := uxTestModel(t, 3)
	m.cmdMode = true
	m.cmdText = "done"
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	next, _ := m.updateInbox(msg)
	after := next.(*Model)
	if len(after.emails) != 2 {
		t.Fatalf(":done left %d emails, want 2 (optimistic removal)", len(after.emails))
	}
	if len(after.optimistic) == 0 {
		t.Fatal(":done did not go through the optimistic path")
	}
}

// ── 2. Filter field tokens ───────────────────────────────────────────────

func TestSplitFilterTokensQuotes(t *testing.T) {
	got := splitFilterTokens(`from:a@b.c after:"3 days ago" plain text`)
	want := []string{"from:a@b.c", "after:3 days ago", "plain", "text"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokens = %v, want %v", got, want)
		}
	}
}

func TestParseFilterQueryTokens(t *testing.T) {
	now := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	aliases := map[string]string{"trash": "Trash", "done": "Archive", "archive": "Archive"}
	q := parseFilterQuery(`from:simon subject:invoice has:attachment before:"2026-03-10" after:"2026-03-05" in:trash plain words`, now, aliases)
	if q.from != "simon" || q.subject != "invoice" || !q.hasAttachment {
		t.Fatalf("parsed = %+v", q)
	}
	if q.before.IsZero() || q.after.IsZero() {
		t.Fatalf("dates not parsed: %+v", q)
	}
	if !q.after.Before(now) || !q.before.After(q.after) {
		t.Fatalf("date order wrong: after=%v before=%v now=%v", q.after, q.before, now)
	}
	if q.inFolder != "Trash" {
		t.Fatalf("in: resolved to %q, want Trash", q.inFolder)
	}
	if q.freeText != "plain words" {
		t.Fatalf("freeText = %q, want %q", q.freeText, "plain words")
	}
}

func TestParseFilterQueryDropsBadDates(t *testing.T) {
	now := time.Now()
	q := parseFilterQuery("before:not-a-date", now, nil)
	if !q.before.IsZero() {
		t.Fatalf("unparsable date should be dropped, got %v", q.before)
	}
	if q.freeText != "" {
		t.Fatalf("dropped date should not leak into free text, got %q", q.freeText)
	}
}

func TestFilterQueryMatchesEmail(t *testing.T) {
	base := time.Date(2026, time.March, 5, 10, 0, 0, 0, time.UTC)
	e := imap.Email{
		From: "alice@example.com", To: "bob@example.com", CC: "carol@example.com",
		Subject: "Quarterly Report", Folder: "INBOX", Date: base, HasAttachment: true,
	}
	yes := filterQuery{from: "alice"}
	if !yes.matchesEmail(e) {
		t.Error("from:alice should match")
	}
	if (filterQuery{from: "bob"}).matchesEmail(e) {
		t.Error("from:bob should not match")
	}
	if !(filterQuery{to: "carol"}).matchesEmail(e) {
		t.Error("to:carol should match Cc")
	}
	if !(filterQuery{subject: "report"}).matchesEmail(e) {
		t.Error("subject:report (case-insensitive) should match")
	}
	if (filterQuery{hasAttachment: true}).matchesEmail(imap.Email{}) {
		t.Error("has:attachment should not match attachment-less email")
	}
	if !(filterQuery{before: base.Add(time.Hour)}).matchesEmail(e) {
		t.Error("before:+1h should match")
	}
	if (filterQuery{before: base}).matchesEmail(e) {
		t.Error("before:date itself should not match (strictly before)")
	}
	if !(filterQuery{after: base.Add(-time.Hour)}).matchesEmail(e) {
		t.Error("after:-1h should match")
	}
	if !(filterQuery{inFolder: "inbox"}).matchesEmail(e) {
		t.Error("in:inbox (case-insensitive path) should match")
	}
	if (filterQuery{inFolder: "Trash"}).matchesEmail(e) {
		t.Error("in:Trash should not match INBOX email")
	}
	// AND combination.
	if (filterQuery{from: "alice", subject: "nope"}).matchesEmail(e) {
		t.Error("from+subject must AND")
	}
}

func TestApplyFilterWithFieldTokens(t *testing.T) {
	m := threadedUXModel(t)
	m.filterActive = true
	m.filterText = "has:attachment"
	m.applyFilter()
	if items := m.inbox.Items(); len(items) != 1 {
		t.Fatalf("has:attachment filter left %d rows, want 1", len(items))
	}

	m.filterText = "in:inbox"
	m.applyFilter()
	if items := m.inbox.Items(); len(items) != 5 {
		t.Fatalf("in:inbox filter left %d rows, want 5", len(items))
	}

	m.filterText = "from:bob"
	m.applyFilter()
	if items := m.inbox.Items(); len(items) != 1 {
		t.Fatalf("from:bob filter left %d rows, want 1", len(items))
	}

	// Combined with free text.
	m.filterText = "from:alice kickoff"
	m.applyFilter()
	if items := m.inbox.Items(); len(items) != 2 {
		t.Fatalf("from:alice kickoff left %d rows, want 2 (thread root + reply)", len(items))
	}
}

func TestFolderAliasesResolveLabelsAndAliases(t *testing.T) {
	m := uxTestModel(t, 1)
	aliases := m.folderAliases()
	for alias, want := range map[string]string{
		"trash": "Trash", "inbox": "INBOX", "done": "Archive",
		"other": "ScreenedOut", "feed": "Feed", "archive": "Archive",
	} {
		if got := aliases[alias]; got != want {
			t.Errorf("alias %q = %q, want %q", alias, got, want)
		}
	}
	if got := resolveFolderAlias("all", aliases); got != "" {
		t.Errorf("in:all should clear the constraint, got %q", got)
	}
}

// ── 3. Focus view ────────────────────────────────────────────────────────

func focusTestModel(t *testing.T, screenedIn []string) (Model, func()) {
	t.Helper()
	dir := t.TempDir()
	listPath := filepath.Join(dir, "screened_in.txt")
	if err := os.WriteFile(listPath, []byte(strings.Join(screenedIn, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := screener.New(screener.Config{ScreenedIn: listPath})
	if err != nil {
		t.Fatal(err)
	}
	m := keysTestModel(t, 4)
	m.emails = keysTestEmails(4)
	for i := range m.emails {
		if i%2 == 0 {
			m.emails[i].From = "vip@important.com"
		}
	}
	m.applyFilter()
	m.screener = sc
	return m, func() { _ = dir }
}

func TestFocusViewTogglesScreenedInOnly(t *testing.T) {
	m, cleanup := focusTestModel(t, []string{"vip@important.com"})
	defer cleanup()

	m = pressLeader(m, "i")
	if !m.focusMode {
		t.Fatal("<space>i did not enable focus mode")
	}
	items := m.inbox.Items()
	if len(items) != 2 {
		t.Fatalf("focus view shows %d rows, want only the 2 screened-in ones", len(items))
	}
	for _, it := range items {
		item := it.(emailItem)
		if item.email.From != "vip@important.com" {
			t.Errorf("focus view kept %q", item.email.From)
		}
	}

	m = pressLeader(m, "i")
	if m.focusMode {
		t.Fatal("<space>i did not toggle focus mode off")
	}
	if items := m.inbox.Items(); len(items) != 4 {
		t.Fatalf("focus off shows %d rows, want 4", len(items))
	}
}

func TestFocusViewCombinesWithUnreadFilter(t *testing.T) {
	m, cleanup := focusTestModel(t, []string{"vip@important.com"})
	defer cleanup()
	m.showUnreadOnly = true
	m = pressLeader(m, "i")
	// All fixture emails are unread, so both filtered views agree here;
	// mark one VIP read to prove the AND.
	for i := range m.emails {
		if m.emails[i].From == "vip@important.com" && i%2 == 0 {
			m.emails[i].Seen = true
			break
		}
	}
	m.applyFilter()
	if items := m.inbox.Items(); len(items) != 1 {
		t.Fatalf("focus + unread shows %d rows, want 1", len(items))
	}
}

// ── 4. Unread thread jumping ─────────────────────────────────────────────

func TestJumpUnreadThreadNextAndPrevious(t *testing.T) {
	m := threadedUXModel(t)
	items := m.inbox.Items()
	// Displayed order (threads sorted by newest desc): invoice(read),
	// Lunch?(unread), then the kickoff thread reply2(unread), reply1(read),
	// root(unread). Blocks: [0] invoice, [1] lunch, [2..4] kickoff thread.
	if len(items) != 5 {
		t.Fatalf("fixture has %d rows, want 5", len(items))
	}

	if got := jumpUnreadThread(items, 0, 1); got != 1 {
		t.Errorf("next unread thread from row 0 = %d, want 1 (Lunch)", got)
	}
	if got := jumpUnreadThread(items, 1, 1); got != 2 {
		t.Errorf("next unread thread from row 1 = %d, want 2 (kickoff top)", got)
	}
	// From the kickoff thread, next wraps past the read invoice to Lunch.
	if got := jumpUnreadThread(items, 3, 1); got != 1 {
		t.Errorf("next unread thread from row 3 = %d, want 1 (wrap)", got)
	}
	if got := jumpUnreadThread(items, 2, -1); got != 1 {
		t.Errorf("previous unread thread from row 2 = %d, want 1", got)
	}
	// Previous from Lunch wraps past the read invoice to the kickoff thread.
	if got := jumpUnreadThread(items, 1, -1); got != 2 {
		t.Errorf("previous unread thread from row 1 = %d, want 2 (wrap)", got)
	}
}

func TestJumpUnreadThreadNoneFound(t *testing.T) {
	m := keysTestModel(t, 2)
	for i := range m.emails {
		m.emails[i].Seen = true
	}
	m.applyFilter()
	if got := jumpUnreadThread(m.inbox.Items(), 0, 1); got != -1 {
		t.Errorf("no unread anywhere: jump = %d, want -1", got)
	}
}

func TestNKeyJumpsBetweenUnreadThreads(t *testing.T) {
	m := threadedUXModel(t)
	// Cursor starts on row 0 (Invoice, read). N must land on Lunch (1),
	// skipping nothing but moving thread-by-thread.
	m = press(m, "N")
	if got := m.inbox.Index(); got != 1 {
		t.Fatalf("N moved cursor to %d, want 1", got)
	}
	// N again lands on the kickoff thread's newest row.
	m = press(m, "N")
	if got := m.inbox.Index(); got != 2 {
		t.Fatalf("N moved cursor to %d, want 2", got)
	}
	// N wraps back to the only other unread thread (Lunch).
	m = press(m, "N")
	if got := m.inbox.Index(); got != 1 {
		t.Fatalf("N wrap moved cursor to %d, want 1", got)
	}
	// <space>p goes back to the previous thread with unread.
	m = pressLeader(m, "p")
	if got := m.inbox.Index(); got != 2 {
		t.Fatalf("<space>p moved cursor to %d, want 2", got)
	}
}

// ── 5. Thread collapsing ─────────────────────────────────────────────────

func TestCollapseThreadRows(t *testing.T) {
	m := threadedUXModel(t)
	threaded := threadEmails(m.emails, "date", true)
	collapsed := collapseThreadRows(threaded, nil)
	if len(collapsed) != 3 {
		t.Fatalf("collapse left %d rows, want 3 (thread + 2 singles)", len(collapsed))
	}
	if collapsed[2].threadCount != 3 {
		t.Errorf("collapsed thread count = %d, want 3", collapsed[2].threadCount)
	}
	if collapsed[2].threadPrefix != "" {
		t.Errorf("collapsed row should lose its thread prefix, got %q", collapsed[2].threadPrefix)
	}
	if collapsed[2].email.UID != 98 {
		t.Errorf("collapsed row should be the newest message (UID 98), got %d", collapsed[2].email.UID)
	}

	// Expanded threads keep all rows.
	expanded := collapseThreadRows(threaded, map[string]bool{"project kickoff": true})
	if len(expanded) != 5 {
		t.Fatalf("expanded thread kept %d rows, want 5", len(expanded))
	}
}

func TestCollapseToggleInInbox(t *testing.T) {
	m := threadedUXModel(t)
	m = pressLeader(m, "t")
	if !m.collapsedThreads {
		t.Fatal("<space>t did not enable collapsing")
	}
	if items := m.inbox.Items(); len(items) != 3 {
		t.Fatalf("collapsed list has %d rows, want 3", len(items))
	}
	// Opening a collapsed row starts the conversation fetch (the Thread
	// off-tab lands with the result, like every other ad-hoc view).
	m.inbox.Select(2)
	if item, ok := selectedEmailItem(m.inbox); !ok || item.threadCount != 3 {
		t.Fatalf("selected row is not the collapsed thread: ok=%v count=%d", ok, item.threadCount)
	}
	m = press(m, "enter")
	if !m.loading {
		t.Fatal("enter on collapsed row did not start the conversation fetch")
	}
	// Toggle off restores all rows.
	m = threadedUXModel(t)
	m = pressLeader(m, "t")
	m = pressLeader(m, "t")
	if m.collapsedThreads {
		t.Fatal("<space>t did not toggle collapsing off")
	}
	if items := m.inbox.Items(); len(items) != 5 {
		t.Fatalf("expanded list has %d rows, want 5", len(items))
	}
}

// ── 6. Snippets: insert + manager ────────────────────────────────────────

func uxSnippetModel(t *testing.T, dir string) Model {
	t.Helper()
	m := keysTestModel(t, 2)
	m.state = stateCompose
	m.prevState = stateCompose
	m.snippetDir = dir
	m.snippets = snippets.Load(dir)
	return m
}

func writeSnippetDir(t *testing.T) (string, []snippets.Snippet) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "standup.md"),
		[]byte("Subject: Standup\n\nToday: shipping the palette.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oneline.txt"),
		[]byte("short and sweet"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, snippets.Load(dir)
}

func TestSnippetInsertAtCursorSingleLine(t *testing.T) {
	dir, _ := writeSnippetDir(t)
	m := uxSnippetModel(t, dir)
	m.compose.step = stepSubject
	m.compose.subject.SetValue("Hello ")
	m.compose.subject.SetCursor(6)
	m.snippetInsert = true
	m.state = stateSnippets

	m.snippets = snippets.Load(dir)
	if idx := snippetIndexByName(m.snippets, "oneline"); idx >= 0 {
		m.snippetsCursor = idx
	}
	next, _ := m.updateSnippets(tea.KeyMsg{Type: tea.KeyEnter})
	after := next.(Model)
	if got := after.compose.subject.Value(); got != "Hello short and sweet" {
		t.Fatalf("subject = %q, want snippet inserted at cursor", got)
	}
	if after.state != stateCompose {
		t.Fatalf("state = %v, want back to compose", after.state)
	}
	if after.snippetInsert {
		t.Error("insert flag should clear after insertion")
	}
}

func TestSnippetInsertMultiLineGoesToBody(t *testing.T) {
	dir, _ := writeSnippetDir(t)
	m := uxSnippetModel(t, dir)
	m.snippetInsert = true
	m.state = stateSnippets
	if idx := snippetIndexByName(m.snippets, "standup"); idx >= 0 {
		m.snippetsCursor = idx
	}
	next, _ := m.updateSnippets(tea.KeyMsg{Type: tea.KeyEnter})
	after := next.(Model)
	if !strings.Contains(after.mailtoBody, "Today: shipping the palette.") {
		t.Fatalf("mailtoBody = %q, want multi-line snippet body", after.mailtoBody)
	}
}

func TestSnippetInsertIntoPresendBody(t *testing.T) {
	dir, _ := writeSnippetDir(t)
	m := uxSnippetModel(t, dir)
	m.state = stateSnippets
	m.prevState = statePresend
	m.pendingSend = &pendingSendData{body: "Existing text."}
	m.snippetInsert = true
	if idx := snippetIndexByName(m.snippets, "oneline"); idx >= 0 {
		m.snippetsCursor = idx
	}
	next, _ := m.updateSnippets(tea.KeyMsg{Type: tea.KeyEnter})
	after := next.(Model)
	if !strings.Contains(after.pendingSend.body, "short and sweet") ||
		!strings.Contains(after.pendingSend.body, "Existing text.") {
		t.Fatalf("presend body = %q, want existing text + snippet", after.pendingSend.body)
	}
}

func snippetIndexByName(snips []snippets.Snippet, name string) int {
	for i, s := range snips {
		if s.Name == name {
			return i
		}
	}
	return -1
}

func TestSnippetManagerCreateEditDelete(t *testing.T) {
	dir := t.TempDir()
	m := uxSnippetModel(t, dir)
	m.snippetsManage = true
	m.state = stateSnippets

	// n opens the name prompt; enter creates the file.
	m = pressSnip(m, "n")
	if !m.snippetNew {
		t.Fatal("n did not open the new-snippet prompt")
	}
	for _, r := range "thanks" {
		m = pressSnip(m, string(r))
	}
	m = pressSnipMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if _, err := os.Stat(filepath.Join(dir, "thanks.md")); err != nil {
		t.Fatalf("new snippet file not created: %v", err)
	}

	// The editor-done reload (simulated) picks up the new file.
	m.snippets = snippets.Load(dir)
	if snippetIndexByName(m.snippets, "thanks") < 0 {
		t.Fatal("thanks.md not listed after create")
	}
	m.snippetsCursor = snippetIndexByName(m.snippets, "thanks")

	// d asks for confirmation; y deletes through the returned Cmd + done msg.
	m = pressSnip(m, "d")
	if !m.snippetDelete {
		t.Fatal("d did not arm delete confirmation")
	}
	if after := pressSnip(m, "n"); after.snippetDelete {
		t.Fatal("n should cancel delete confirmation")
	}
	m = pressSnip(m, "d")
	next, cmd := m.updateSnippets(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y did not produce a delete cmd")
	}
	msg := cmd() // snippetEditDoneMsg
	if _, ok := msg.(snippetEditDoneMsg); !ok {
		t.Fatalf("delete cmd produced %T", msg)
	}
	after := next.(Model)
	updated, _ := after.Update(msg)
	if _, err := os.Stat(filepath.Join(dir, "thanks.md")); !os.IsNotExist(err) {
		t.Fatal("thanks.md still present after delete")
	}
	_ = updated
}

func pressSnip(m Model, key string) Model {
	return pressSnipMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

func pressSnipMsg(m Model, msg tea.KeyMsg) Model {
	next, _ := m.updateSnippets(msg)
	return next.(Model)
}

// ── 7. Undo toast ────────────────────────────────────────────────────────

func TestUndoActionDescribe(t *testing.T) {
	a := undoAction{moves: []undoMove{{uid: 1, fromFolder: "Archive", toFolder: "INBOX"}}}
	if got := a.describe(); !strings.Contains(got, "1 email(s) back to Archive") {
		t.Errorf("describe moves = %q", got)
	}
	b := undoAction{seen: []seenFlag{{folder: "INBOX", uid: 2}}}
	if got := b.describe(); !strings.Contains(got, "read state") {
		t.Errorf("describe seen = %q", got)
	}
	c := undoAction{
		moves: []undoMove{{uid: 1, toFolder: "INBOX"}},
		seen:  []seenFlag{{folder: "INBOX", uid: 2}},
	}
	if got := c.describe(); !strings.Contains(got, " and ") {
		t.Errorf("combined describe = %q, want both parts", got)
	}
}

// ── 8. Compose autocomplete fuzzy tier ──────────────────────────────────

func TestComposeSuggestionsFuzzySubsequence(t *testing.T) {
	c := newComposeModel()
	cs := contacts.Load("")
	cs.Add("max@example.com", "Max Muster")
	c.contacts = cs
	c.step = stepTo
	c.to.SetValue("mm")
	c.updateSuggestions()
	found := false
	for _, s := range c.suggestions {
		if strings.Contains(s, "Max Muster") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fuzzy query mm did not suggest Max Muster: %v", c.suggestions)
	}
}

// ── 9. Reminder due times in the list ───────────────────────────────────

func TestReminderDueTimeShownInListRow(t *testing.T) {
	m := keysTestModel(t, 1)
	m.emails[0].Reminder = &reminder.Metadata{
		At: time.Date(2026, time.March, 15, 14, 30, 0, 0, time.UTC), State: "waiting",
	}
	m.applyFilter()
	var out strings.Builder
	delegate := emailDelegate{}
	delegate.Render(&out, m.inbox, m.inbox.Index(), m.inbox.Items()[m.inbox.Index()])
	want := time.Date(2026, time.March, 15, 14, 30, 0, 0, time.UTC).Local().Format("Jan 2 15:04")
	if !strings.Contains(out.String(), want) {
		t.Fatalf("row render missing reminder time %q: %q", want, out.String())
	}
}
