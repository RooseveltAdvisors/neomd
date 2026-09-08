package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/config"
	"github.com/sspaeti/neomd/internal/snippets"
)

// ── Snippets picker (`;`) / manager (`:snip`) / insert (<space>;) ────────
//
// Reusable email templates read from <config dir>/snippets/*.md. j/k moves,
// enter starts a compose pre-filled with the snippet's subject and body,
// esc/q closes. Loaded on open so a newly-added file shows up without a
// restart.
//
// Two extra modes:
//   - insert (opened via <space>; from compose/pre-send): enter inserts the
//     snippet body at the cursor of the focused compose field (single-line
//     bodies) or into the message body (multi-line).
//   - manager (opened via :snip or m in the picker): n creates, e/enter
//     edits in $EDITOR, d deletes (y/n), esc returns to the previous view.

// openSnippetsCmd loads the snippet directory and enters the picker.
func (m Model) openSnippets() (tea.Model, tea.Cmd) {
	m.snippets = m.reloadSnippets()
	m.snippetsCursor = 0
	m.prevState = m.state
	m.state = stateSnippets
	m.snippetInsert = false
	m.snippetsManage = false
	m.snippetNew = false
	m.snippetNewName = ""
	m.snippetDelete = false
	m.status = ""
	m.isError = false
	return m, nil
}

// openSnippetInsert opens the picker in insert mode: enter inserts the
// snippet into the compose field at the cursor / the message body.
func (m Model) openSnippetInsert() (tea.Model, tea.Cmd) {
	mm, cmd := m.openSnippets()
	model := mm.(Model)
	model.snippetInsert = true
	return model, cmd
}

// openSnippetManager opens the snippet manager screen (create/edit/delete).
func (m Model) openSnippetManager() (tea.Model, tea.Cmd) {
	mm, cmd := m.openSnippets()
	model := mm.(Model)
	model.snippetsManage = true
	return model, cmd
}

// reloadSnippets re-reads the snippet directory into the model.
func (m *Model) reloadSnippets() []snippets.Snippet {
	m.snippets = snippets.Load(m.snippetsDir())
	return m.snippets
}

// snippetEditDoneMsg is emitted after a snippet file operation finishes
// (editor closed or file deleted).
type snippetEditDoneMsg struct{ err error }

// snippetPathIn returns the snippet file path under dir.
func snippetPathIn(dir string, s snippets.Snippet) string {
	return filepath.Join(dir, s.Name+".md")
}

// snippetsDir resolves the snippet directory (test-overridable via the
// snippetDir field).
func (m Model) snippetsDir() string {
	if m.snippetDir != "" {
		return m.snippetDir
	}
	return config.SnippetsDir()
}

// snippetEditPath returns the file path of the given snippet.
func (m Model) snippetEditPath(s snippets.Snippet) string {
	return snippetPathIn(m.snippetsDir(), s)
}

// snippetEditorCmd opens $EDITOR on the given snippet file and reloads the
// list afterwards.
func (m Model) snippetEditorCmd(path string) tea.Cmd {
	editorBin := os.Getenv("EDITOR")
	if editorBin == "" {
		editorBin = "nvim"
	}
	return tea.ExecProcess(exec.Command(editorBin, path), func(err error) tea.Msg {
		return snippetEditDoneMsg{err: err}
	})
}

// snippetNewCmd creates a named snippet file (with a Subject header stub)
// and opens it in $EDITOR.
func (m Model) snippetNewCmd(name string) tea.Cmd {
	return m.snippetNewIn(m.snippetsDir(), name)
}

// snippetNewIn is snippetNewCmd against an explicit directory (testable).
func (m Model) snippetNewIn(dir, name string) tea.Cmd {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.status = "create snippets dir: " + err.Error()
		m.isError = true
		return nil
	}
	path := filepath.Join(dir, name+".md")
	if _, err := os.Stat(path); err == nil {
		m.status = fmt.Sprintf("Snippet %q already exists — editing it.", name)
	} else if err := os.WriteFile(path, []byte("Subject: \n\n"), 0o600); err != nil {
		m.status = "create snippet: " + err.Error()
		m.isError = true
		return nil
	}
	return m.snippetEditorCmd(path)
}

// snippetDeleteCmd removes the given snippet's file.
func (m Model) snippetDeleteCmd(s snippets.Snippet) tea.Cmd {
	return func() tea.Msg {
		return snippetEditDoneMsg{err: os.Remove(m.snippetEditPath(s))}
	}
}

// snippetDeleteIn is snippetDeleteCmd against an explicit directory (testable).
func snippetDeleteIn(dir string, s snippets.Snippet) error {
	return os.Remove(snippetPathIn(dir, s))
}

// insertSnippet inserts the snippet in the current compose/pre-send
// context: at the cursor of the focused single-line field when the body
// fits on one line, otherwise into the message body.
func (m Model) insertSnippet(s snippets.Snippet) (tea.Model, tea.Cmd) {
	body := s.Body
	m.snippetInsert = false
	m.state = m.prevState
	switch m.state {
	case statePresend:
		if m.pendingSend == nil {
			return m, nil
		}
		m.pendingSend.body = strings.TrimRight(m.pendingSend.body, "\n") + "\n\n" + body
		m.status = fmt.Sprintf("Snippet %q inserted into the message body.", s.Name)
		return m, nil
	default:
		field := m.compose.activeField()
		if field != nil && !strings.Contains(body, "\n") {
			value := field.Value()
			pos := field.Position()
			if pos > len(value) {
				pos = len(value)
			}
			field.SetValue(value[:pos] + body + value[pos:])
			field.SetCursor(pos + len(body))
			m.status = fmt.Sprintf("Snippet %q inserted at cursor.", s.Name)
			return m, nil
		}
		m.mailtoBody = strings.TrimSpace(m.mailtoBody + "\n\n" + body)
		m.status = fmt.Sprintf("Snippet %q added to the email body (opens in the editor).", s.Name)
		return m, nil
	}
}

func (m Model) updateSnippets(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// New-snippet name prompt (manager mode).
	if m.snippetNew {
		switch msg.String() {
		case "esc":
			m.snippetNew = false
			m.snippetNewName = ""
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.snippetNewName)
			if name == "" {
				m.status = "Snippet name required."
				m.isError = true
				return m, nil
			}
			m.snippetNew = false
			m.snippetNewName = ""
			return m, m.snippetNewCmd(name)
		case "backspace", "ctrl+h":
			if r := []rune(m.snippetNewName); len(r) > 0 {
				m.snippetNewName = string(r[:len(r)-1])
			}
			return m, nil
		default:
			if k := msg.String(); len(k) == 1 {
				m.snippetNewName += k
			}
			return m, nil
		}
	}
	// Delete confirmation (manager mode).
	if m.snippetDelete {
		switch msg.String() {
		case "y":
			m.snippetDelete = false
			if m.snippetsCursor < len(m.snippets) {
				return m, m.snippetDeleteCmd(m.snippets[m.snippetsCursor])
			}
			return m, nil
		case "n", "esc":
			m.snippetDelete = false
			m.status = ""
			return m, nil
		default:
			return m, nil
		}
	}
	switch msg.String() {
	case "esc", "q", ";":
		insert := m.snippetInsert
		m.snippetInsert = false
		if m.snippetsManage {
			m.snippetsManage = false
			return m, nil
		}
		m.state = m.prevState
		if insert {
			m.status = ""
		}
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
		if m.snippetsManage {
			return m, m.snippetEditorCmd(m.snippetEditPath(m.snippets[m.snippetsCursor]))
		}
		if m.snippetInsert {
			return m.insertSnippet(m.snippets[m.snippetsCursor])
		}
		return m.composeFromSnippet(m.snippets[m.snippetsCursor])
	}
	if m.snippetsManage {
		switch msg.String() {
		case "n":
			m.snippetNew = true
			m.snippetNewName = ""
			return m, nil
		case "e":
			if m.snippetsCursor < len(m.snippets) {
				return m, m.snippetEditorCmd(m.snippetEditPath(m.snippets[m.snippetsCursor]))
			}
			return m, nil
		case "d":
			if m.snippetsCursor < len(m.snippets) {
				m.snippetDelete = true
				m.status = ""
			}
			return m, nil
		}
	} else if msg.String() == "m" && !m.snippetInsert {
		// Picker: m opens the manager.
		return m.openSnippetManager()
	}
	// Number keys 1-9 pick directly.
	if k := msg.String(); len(k) == 1 && k >= "1" && k <= "9" {
		i := int(k[0] - '1')
		if i < len(m.snippets) {
			if m.snippetInsert {
				return m.insertSnippet(m.snippets[i])
			}
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
	title := " Snippets "
	hints := "  j/k move · 1-9 pick · enter compose · m manage · esc close"
	if m.snippetInsert {
		title = " Insert snippet "
		hints = "  j/k move · 1-9 insert · enter insert at cursor · esc cancel"
	}
	if m.snippetsManage {
		title = " Snippet manager "
		hints = "  n new · e/enter edit · d delete · esc done"
	}
	b.WriteString(styleHeader.Render(title) + "\n\n")
	if m.snippetNew {
		b.WriteString("  Name: " + m.snippetNewName + "█\n\n")
		b.WriteString(styleHelp.Render("  enter create · esc cancel"))
		return b.String()
	}
	if len(m.snippets) == 0 {
		b.WriteString(styleHelp.Render("  No snippets yet.\n\n  Drop markdown files in " + config.SnippetsDir() + "/\n" +
			"  An optional first line `Subject: ...` sets the subject.\n"))
		if m.snippetsManage {
			b.WriteString("\n" + styleHelp.Render("  n create a new snippet · esc done"))
		} else {
			b.WriteString("\n" + styleHelp.Render("  esc close"))
		}
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
	if m.snippetDelete {
		s := m.snippets[m.snippetsCursor]
		b.WriteString("\n  Delete snippet \"" + s.Name + "\"? y/n\n")
	}
	b.WriteString("\n" + styleHelp.Render(hints))
	return b.String()
}
