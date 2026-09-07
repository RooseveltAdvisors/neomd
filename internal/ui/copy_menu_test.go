package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/imap"
)

func TestReaderYOpensCopyMenu(t *testing.T) {
	m := Model{
		state:     stateReading,
		openEmail: &imap.Email{MessageID: "<abc@example.com>"},
		width:     80,
		height:    24,
	}
	updated, cmd := m.updateReader(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd != nil {
		t.Fatal("y should open the copy menu without starting a copy")
	}
	got := updated.(Model)
	if got.state != stateCopyMenu || len(got.copyTargetsList) != 1 {
		t.Fatalf("y state=%v targets=%d, want copy menu with one target", got.state, len(got.copyTargetsList))
	}
	if got.copyTargetsList[0].text != "neomd://mid/%3Cabc@example.com%3E" {
		t.Fatalf("copy target = %q", got.copyTargetsList[0].text)
	}
}

func TestCopyMenuMessageIDBinding(t *testing.T) {
	m := Model{
		state:     stateReading,
		openEmail: &imap.Email{MessageID: "<abc@example.com>"},
	}
	opened, _ := m.updateReader(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	selected, cmd := opened.(Model).updateCopyMenu(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if selected.(Model).state != stateReading {
		t.Fatal("selecting a copy target should return to the reader")
	}
	msg, ok := cmd().(clipboardDoneMsg)
	if !ok {
		t.Fatalf("copy command returned %T, want clipboardDoneMsg", msg)
	}
	if msg.text != "neomd://mid/%3Cabc@example.com%3E" {
		t.Fatalf("copied text = %q", msg.text)
	}
}
