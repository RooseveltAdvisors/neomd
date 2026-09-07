package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/contacts"
)

// ── Contacts picker (space c) ────────────────────────────────────────────
//
// Browses the merged address book (harvested names + [contacts] file):
// `/` filters by name or address, j/k move, `y` copies the address to the
// clipboard, `Y` copies "Name <addr>", enter starts a compose to the
// selected contact, esc/q closes.

// filteredContacts returns the address book filtered by the picker's query
// (case-insensitive substring on name or address).
func (m Model) filteredContacts() []contacts.Entry {
	all := m.contacts.All()
	q := strings.ToLower(strings.TrimSpace(m.contactsFilter))
	if q == "" {
		return all
	}
	var out []contacts.Entry
	for _, e := range all {
		if strings.Contains(strings.ToLower(e.Name), q) || strings.Contains(e.Addr, q) {
			out = append(out, e)
		}
	}
	return out
}

func (m Model) updateContacts(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	entries := m.filteredContacts()
	clamp := func() {
		if m.contactsCursor >= len(entries) {
			m.contactsCursor = len(entries) - 1
		}
		if m.contactsCursor < 0 {
			m.contactsCursor = 0
		}
	}
	clamp()

	// Filter typing mode: `/` opens it, printable keys extend the query.
	if m.contactsFilterActive {
		switch key {
		case "esc":
			if m.contactsFilter != "" {
				m.contactsFilter = ""
				m.contactsCursor = 0
			} else {
				m.contactsFilterActive = false
			}
			return m, nil
		case "enter":
			m.contactsFilterActive = false
			return m, nil
		case "backspace", "ctrl+h":
			if r := []rune(m.contactsFilter); len(r) > 0 {
				m.contactsFilter = string(r[:len(r)-1])
				m.contactsCursor = 0
			}
			return m, nil
		default:
			if len([]rune(key)) == 1 {
				m.contactsFilter += key
				m.contactsCursor = 0
			}
			return m, nil
		}
	}

	switch key {
	case "esc", "q":
		m.state = m.prevState
		return m, nil
	case "/":
		m.contactsFilterActive = true
		return m, nil
	case "j", "down":
		if m.contactsCursor < len(entries)-1 {
			m.contactsCursor++
		}
	case "k", "up":
		if m.contactsCursor > 0 {
			m.contactsCursor--
		}
	case "G":
		m.contactsCursor = len(entries) - 1
		clamp()
	case "g":
		m.contactsCursor = 0
	case "y", "Y":
		if len(entries) == 0 {
			return m, nil
		}
		e := entries[m.contactsCursor]
		text := e.Addr
		if key == "Y" {
			text = e.Name + " <" + e.Addr + ">"
		}
		return m, copyToClipboardCmd(text)
	case "enter", "c":
		if len(entries) == 0 {
			return m, nil
		}
		// Start a compose to the selected contact (same prefill as mailto).
		addr := entries[m.contactsCursor].Addr
		m.attachments = nil
		m.compose.reset()
		m.presendFromI = m.defaultFromIndex()
		m.compose.to.SetValue(addr)
		m.state = stateCompose
		m.status = ""
		m.isError = false
		return m, nil
	}
	return m, nil
}

func (m Model) viewContacts() string {
	entries := m.filteredContacts()
	var b strings.Builder
	b.WriteString(styleHeader.Render(fmt.Sprintf("  Contacts (%d)", len(entries))) + "\n")
	b.WriteString(styleSeparator.Render(strings.Repeat("─", m.width)) + "\n")

	// Visible window around the cursor.
	rows := m.height - 6
	if rows < 3 {
		rows = 3
	}
	start := 0
	if m.contactsCursor >= rows {
		start = m.contactsCursor - rows + 1
	}
	end := start + rows
	if end > len(entries) {
		end = len(entries)
	}
	nameW := 0
	for _, e := range entries[start:end] {
		if len(e.Name) > nameW {
			nameW = len(e.Name)
		}
	}
	for i := start; i < end; i++ {
		e := entries[i]
		line := fmt.Sprintf("  %-*s  %s", nameW, e.Name, e.Addr)
		if i == m.contactsCursor {
			line = styleSelected.Render("▸" + line[1:])
		}
		b.WriteString(line + "\n")
	}
	if len(entries) == 0 {
		b.WriteString(styleHelp.Render("  no contacts match — names are harvested from email headers and the [contacts] file") + "\n")
	}

	b.WriteString("\n")
	if m.contactsFilterActive {
		b.WriteString(styleInputLabel.Render("filter:") + " " + m.contactsFilter + "█")
	} else if m.status != "" {
		b.WriteString(statusBar(m.status, m.isError))
	} else {
		filterHint := ""
		if m.contactsFilter != "" {
			filterHint = fmt.Sprintf("  filter: %q ·", m.contactsFilter)
		}
		b.WriteString(styleHelp.Render(fmt.Sprintf(" %s j/k move · / filter · y copy address · Y copy \"Name <addr>\" · enter compose · esc close", filterHint)))
	}
	return b.String()
}
