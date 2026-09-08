package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func filterKey(m Model, key string) Model {
	var msg tea.KeyMsg
	if key == "enter" {
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	} else {
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.updateInbox(msg)
	return next.(Model)
}

func commitInboxFilter(m Model, query string) Model {
	m = filterKey(m, "/")
	for _, r := range query {
		m = filterKey(m, string(r))
	}
	return filterKey(m, "enter")
}

func filteredActionModel(t *testing.T) Model {
	t.Helper()
	m := keysTestModel(t, 5)
	m.emails[0].Subject = "ordinary one"
	m.emails[1].Subject = "ordinary two"
	m.emails[2].Subject = "needle target"
	m.emails[3].Subject = "needle next"
	m.emails[4].Subject = "ordinary four"
	m.applyFilter()
	// Leave the full-list cursor away from zero. A stale underlying index would
	// select the wrong row once the filter narrows the list.
	m.inbox.Select(3)
	return commitInboxFilter(m, "needle")
}

func TestCommittedFilterTargetsHighlightedFilteredEmail(t *testing.T) {
	m := filteredActionModel(t)
	if m.filterActive || m.filterText != "needle" {
		t.Fatalf("filter state = active %v text %q", m.filterActive, m.filterText)
	}
	got := selectedEmail(m.inbox)
	if got == nil || got.UID != 97 {
		t.Fatalf("selected filtered email = %#v, want UID 97", got)
	}
	targets := m.targetEmails()
	if len(targets) != 1 || targets[0].UID != 97 {
		t.Fatalf("targets = %#v, want only filtered UID 97", targets)
	}
}

func TestCommittedFilterArchiveAdvancesVisibleRows(t *testing.T) {
	m := filteredActionModel(t)
	next, _ := m.updateInbox(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := next.(Model)
	if len(got.emails) != 4 {
		t.Fatalf("full email count = %d, want 4", len(got.emails))
	}
	if got.inbox.Index() >= len(got.inbox.Items()) {
		t.Fatalf("cursor %d is outside filtered list of %d", got.inbox.Index(), len(got.inbox.Items()))
	}
	if selected := selectedEmail(got.inbox); selected == nil || selected.UID != 98 {
		t.Fatalf("selected after archive = %#v, want next visible row", selected)
	}
}

func TestCommittedFilterEscClearsResults(t *testing.T) {
	m := filteredActionModel(t)
	next, _ := m.updateInbox(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(Model)
	if got.filterText != "" || got.filterActive || len(got.inbox.Items()) != len(got.emails) {
		t.Fatalf("filter after esc = active %v text %q items=%d full=%d", got.filterActive, got.filterText, len(got.inbox.Items()), len(got.emails))
	}
}
