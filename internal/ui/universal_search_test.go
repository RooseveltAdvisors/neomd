package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/config"
	"github.com/sspaeti/neomd/internal/imap"
	fulltext "github.com/sspaeti/neomd/internal/search"
)

func TestUniversalSearchInputIsCancelable(t *testing.T) {
	cfg := &config.Config{}
	m := Model{cfg: cfg, clients: []*imap.Client{imap.New(imap.Config{Host: "fixture"})}, searchIndex: fulltext.New(), inbox: newInboxList(80, 10, "Sent", "Drafts")}
	next, _ := m.updateInbox(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	got := next.(Model)
	if !got.universalSearchActive {
		t.Fatal("/ did not open universal search")
	}
	got.filterText = "fixture query"
	next, _ = got.updateInbox(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(Model)
	if got.universalSearchActive || got.filterText != "" || got.status != "Universal search canceled." {
		t.Fatalf("cancel state = active=%v text=%q status=%q", got.universalSearchActive, got.filterText, got.status)
	}

	m = Model{cfg: cfg, clients: []*imap.Client{imap.New(imap.Config{Host: "fixture"})}, searchIndex: fulltext.New(), inbox: newInboxList(80, 10, "Sent", "Drafts"), filterText: "fixture"}
	m.universalSearchActive = true
	next, cmd := m.updateInbox(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !next.(Model).universalSearchRunning {
		t.Fatal("enter did not start a cancellable search")
	}
	next, _ = next.(Model).updateInbox(tea.KeyMsg{Type: tea.KeyEsc})
	if !next.(Model).universalSearchRunning || next.(Model).status != "Canceling universal search…" {
		t.Fatalf("running cancel state = %#v", next.(Model))
	}
}

func TestUniversalSearchResultShowsPartialStateAndKeepsResultActionable(t *testing.T) {
	e := imap.Email{Account: "Work", Folder: "Archive", UID: 42, Subject: "fixture result", Date: time.Now()}
	m := Model{inbox: newInboxList(80, 10, "Sent", "Drafts"), searchIndex: fulltext.New(), sortField: "date", sortReverse: true}
	m.universalSearchResults = false
	next, _ := m.handleUniversalSearchResult(universalSearchResultMsg{
		emails: eSlice(e), query: "fixture", indexed: 1, bodies: 0, total: 2, failedScopes: 1, failedBodies: 1,
	})
	got := next.(*Model)
	if !got.universalSearchResults || got.offTabFolder != "Search" || len(got.inbox.Items()) != 1 {
		t.Fatalf("result state = results=%v tab=%q items=%d", got.universalSearchResults, got.offTabFolder, len(got.inbox.Items()))
	}
	if got.status == "" || !containsAll(got.status, "partial", "folder failures", "body failures") {
		t.Fatalf("partial status = %q", got.status)
	}
}

func TestSearchResultUsesOwningAccountForMessageClient(t *testing.T) {
	personal := imap.New(imap.Config{Host: "personal"})
	work := imap.New(imap.Config{Host: "work"})
	cfg := &config.Config{Accounts: []config.AccountConfig{{Name: "Personal"}, {Name: "Work"}}}
	m := Model{cfg: cfg, accounts: cfg.ActiveAccounts(), clients: []*imap.Client{personal, work}}
	e := &imap.Email{Account: "Work", Folder: "Archive", UID: 42}
	if got := m.imapCliForEmail(e); got != work {
		t.Fatalf("message client = %p, want work client %p", got, work)
	}
}

func eSlice(e imap.Email) []imap.Email { return []imap.Email{e} }

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		found := false
		for i := 0; i+len(needle) <= len(s); i++ {
			if s[i:i+len(needle)] == needle {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
