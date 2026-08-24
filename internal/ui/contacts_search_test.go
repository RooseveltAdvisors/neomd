package ui

import (
	"path/filepath"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/contacts"
)

func testStore(t *testing.T) *contacts.Store {
	t.Helper()
	s := contacts.Load(filepath.Join(t.TempDir(), "contacts"))
	s.HarvestField("Louise Nachname <lnachname@domain.io>")
	return s
}

// Searching a contact's name must also query their known address — sent
// messages carry only the bare address in To, so the name itself never
// appears in the headers IMAP SEARCH can match.
func TestExpandSearchQueries(t *testing.T) {
	s := testStore(t)
	cases := []struct {
		query string
		want  []string
	}{
		{"louise", []string{"louise", "lnachname@domain.io"}},
		{"to:Louise", []string{"to:Louise", "to:lnachname@domain.io"}},
		{"from:louise", []string{"from:louise", "from:lnachname@domain.io"}},
		{"subject:louise", []string{"subject:louise"}},
		{"nobody", []string{"nobody"}},
	}
	for _, c := range cases {
		if got := expandSearchQueries(c.query, s); !reflect.DeepEqual(got, c.want) {
			t.Errorf("expandSearchQueries(%q) = %v, want %v", c.query, got, c.want)
		}
	}
	// Nil store (tests construct Model directly) must not panic or expand.
	if got := expandSearchQueries("louise", nil); !reflect.DeepEqual(got, []string{"louise"}) {
		t.Errorf("nil store expansion = %v", got)
	}
}

// The local filter haystack includes resolved contact names for bare addresses.
func TestContactNamesForResolvesBareAddresses(t *testing.T) {
	m := Model{contacts: testStore(t)}
	got := m.contactNamesFor("lnachname@domain.io, other@x.io")
	if got != " louise nachname" {
		t.Errorf("contactNamesFor = %q, want %q", got, " louise nachname")
	}
	var empty Model
	if got := empty.contactNamesFor("lnachname@domain.io"); got != "" {
		t.Errorf("nil contacts: got %q", got)
	}
}

// Contacts picker: / filters by name or address, y yields a copy command,
// enter prefills a compose To the selected contact.
func TestContactsPickerFilterAndSelect(t *testing.T) {
	s := testStore(t)
	s.HarvestField("Bob Builder <bob@x.io>")
	m := Model{contacts: s, compose: newComposeModel(), height: 20, width: 80}

	m.contactsFilter = "louise"
	got := m.filteredContacts()
	if len(got) != 1 || got[0].Addr != "lnachname@domain.io" {
		t.Fatalf("filteredContacts = %v", got)
	}

	// y on the selected entry returns a clipboard command (not nil).
	mm, cmd := m.updateContacts(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Error("y must return a copy command")
	}
	// enter starts a compose with To prefilled.
	mm2, _ := mm.(Model).updateContacts(tea.KeyMsg{Type: tea.KeyEnter})
	res := mm2.(Model)
	if res.state != stateCompose || res.compose.to.Value() != "lnachname@domain.io" {
		t.Errorf("enter: state=%v to=%q", res.state, res.compose.to.Value())
	}
}
