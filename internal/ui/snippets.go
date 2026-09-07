package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/config"
	"github.com/sspaeti/neomd/internal/snippets"
)

// ── Snippets picker (`;`) ────────────────────────────────────────────────
//
// Reusable email templates read from <config dir>/snippets/*.md. j/k moves,
// enter starts a compose pre-filled with the snippet's subject and body,
// esc/q closes. Loaded on open so a newly-added file shows up without a
// restart.

// openSnippetsCmd loads the snippet directory and enters the picker.
func (m Model) openSnippets() (tea.Model, tea.Cmd) {
	m.snippets = snippets.Load(config.SnippetsDir())
	m.snippetsCursor = 0
	m.prevState = m.state
	m.state = stateSnippets
	m.status = ""
	m.isError = false
	return m, nil
}

func (m Model) updateSnippets(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", ";":
		m.state = m.prevState
		return m, nil
	case "j", "down":
		if m.snippetsCursor < len(m.snippets)-1 {
			m.snippetsCursor++
		}
		return m, nil
	case "k", "up":
		if m.snippetsCursor > 0 {
			m.snippetsCursor--
		}
		return m, nil
	case "enter":
		if m.snippetsCursor >= len(m.snippets) {
			return m, nil
		}
		return m.composeFromSnippet(m.snippets[m.snippetsCursor])
	}
	// Number keys 1-9 pick directly.
	if k := msg.String(); len(k) == 1 && k >= "1" && k <= "9" {
		i := int(k[0] - '1')
		if i < len(m.snippets) {
			return m.composeFromSnippet(m.snippets[i])
		}
	}
	return m, nil
}

// composeFromSnippet opens the compose form with the template's subject, and
// stashes the body so launchEditorCmd drops it into the editor buffer (the
// same path a mailto: body takes).
func (m Model) composeFromSnippet(s snippets.Snippet) (tea.Model, tea.Cmd) {
	m.attachments = nil
	m.compose.reset()
	m.presendFromI = m.defaultFromIndex()
	m.compose.subject.SetValue(s.Subject)
	m.mailtoBody = s.Body
	m.state = stateCompose
	m.status = "Snippet: " + s.Name
	m.isError = false
	return m, nil
}

func (m Model) viewSnippets() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render(" Snippets ") + "\n\n")
	if len(m.snippets) == 0 {
		b.WriteString(styleHelp.Render("  No snippets yet.\n\n  Drop markdown files in " + config.SnippetsDir() + "/\n" +
			"  An optional first line `Subject: ...` sets the subject.\n"))
		b.WriteString("\n" + styleHelp.Render("  esc close"))
		return b.String()
	}
	for i, s := range m.snippets {
		cursor := "  "
		name := s.Name
		if i == m.snippetsCursor {
			cursor = "▸ "
			name = styleSelected.Render(name)
		}
		num := "  "
		if i < 9 {
			num = string(rune('1'+i)) + " "
		}
		line := cursor + styleHelp.Render(num) + name
		if s.Subject != "" {
			line += styleHelp.Render("  — " + s.Subject)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + styleHelp.Render("  j/k move · 1-9 pick · enter compose · esc close"))
	return b.String()
}
