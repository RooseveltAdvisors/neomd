package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/imap"
)

func TestReminderPopupQuickPicksAndNaturalLanguage(t *testing.T) {
	m := keysTestModel(t, 2)
	m.width, m.height = 100, 30
	m = press(m, "h")
	if !m.reminderActive || m.reminderQuickPick != -1 {
		t.Fatalf("opened reminder = active %v quick=%d", m.reminderActive, m.reminderQuickPick)
	}

	next, _ := m.updateInbox(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = next.(Model)
	if m.reminderInput.Value() != "tomorrow morning" || m.reminderQuickPick != 1 {
		t.Fatalf("quick pick = %q index %d", m.reminderInput.Value(), m.reminderQuickPick)
	}
	if at, err := m.reminderPreview(); err != nil || at.IsZero() {
		t.Fatalf("quick preview = %v, err=%v", at, err)
	}
	if view := m.View(); !strings.Contains(view, "Remind me in") || !strings.Contains(view, "Tomorrow morning") {
		t.Fatalf("popup view omitted title or quick pick: %q", view)
	}

	// Typing after a quick pick returns to free text and does not let the
	// picker consume letters from a natural-language phrase.
	m.reminderInput.SetValue("")
	for _, r := range "next monday" {
		next, _ = m.updateInbox(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	if m.reminderQuickPick != -1 || m.reminderInput.Value() != "next monday" {
		t.Fatalf("free text = %q quick=%d", m.reminderInput.Value(), m.reminderQuickPick)
	}
	if _, err := m.reminderTime(); err != nil {
		t.Fatalf("natural-language reminder rejected: %v", err)
	}
}

func TestReminderPopupRejectsInvalidInputWithoutClosing(t *testing.T) {
	m := keysTestModel(t, 1)
	m = press(m, "h")
	m.reminderInput.SetValue("sometime")
	next, _ := m.updateInbox(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if !got.reminderActive || got.reminderError == "" {
		t.Fatalf("invalid reminder closed or lacked error: active=%v error=%q", got.reminderActive, got.reminderError)
	}
}

func TestCommittedFilterActionsUseVisibleTarget(t *testing.T) {
	keys := []struct {
		name  string
		key   string
		check func(t *testing.T, before, after Model)
	}{
		{"archive", "e", func(t *testing.T, before, after Model) {
			if containsUID(after.emails, 97) {
				t.Fatal("archive acted on the wrong full-list email")
			}
		}},
		{"remind", "h", func(t *testing.T, _, after Model) {
			if !after.reminderActive || len(after.reminderTargets) != 1 || after.reminderTargets[0].UID != 97 {
				t.Fatalf("reminder target = %#v", after.reminderTargets)
			}
		}},
		{"select", "x", func(t *testing.T, _, after Model) {
			if !after.markedUIDs[97] {
				t.Fatalf("selected UIDs = %#v", after.markedUIDs)
			}
		}},
		{"screen-in", "i", func(t *testing.T, _, after Model) {
			if containsUID(after.emails, 97) {
				t.Fatal("screen-in acted on the wrong full-list email")
			}
		}},
		{"open", "o", func(t *testing.T, _, after Model) {
			if !after.loading {
				t.Fatal("open did not use the filtered row")
			}
		}},
		{"compose", "s", func(t *testing.T, _, after Model) {
			if after.state != stateCompose {
				t.Fatal("compose action did not fire after filtering")
			}
		}},
		{"reply", "r", func(t *testing.T, _, after Model) {
			if !after.pendingReply || !after.loading {
				t.Fatal("reply action did not fire after filtering")
			}
		}},
		{"forward", "f", func(t *testing.T, _, after Model) {
			if !after.pendingForward || !after.loading {
				t.Fatal("forward action did not fire after filtering")
			}
		}},
		{"mark", "m", func(t *testing.T, _, after Model) {
			if !after.markedUIDs[97] {
				t.Fatalf("mark action selected UIDs = %#v", after.markedUIDs)
			}
		}},
		{"visual", "V", func(t *testing.T, _, after Model) {
			if !after.visualSelect || !after.markedUIDs[97] {
				t.Fatalf("visual action = visual %v marked %#v", after.visualSelect, after.markedUIDs)
			}
		}},
	}
	for _, tt := range keys {
		t.Run(tt.name, func(t *testing.T) {
			before := filteredActionModel(t)
			next, _ := before.updateInbox(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			tt.check(t, before, next.(Model))
		})
	}

	before := filteredActionModel(t)
	first, _ := before.updateInbox(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	second, _ := first.(Model).updateInbox(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if containsUID(second.(Model).emails, 97) {
		t.Fatal("dd trash acted on the wrong full-list email")
	}
}

func containsUID(emails []imap.Email, uid uint32) bool {
	for _, e := range emails {
		if e.UID == uid {
			return true
		}
	}
	return false
}
