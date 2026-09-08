package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sspaeti/neomd/internal/when"
)

type reminderChoice struct {
	label string
	input string
}

func reminderChoices() []reminderChoice {
	return []reminderChoice{
		{label: "Later today", input: "later today"},
		{label: "Tomorrow morning", input: "tomorrow morning"},
		{label: "Tomorrow afternoon", input: "tomorrow afternoon"},
		{label: "This weekend", input: "this weekend"},
		{label: "Next week", input: "next week"},
		{label: "In 1 week", input: "in 1 week"},
	}
}

func (m Model) reminderTime() (time.Time, error) {
	input := strings.TrimSpace(m.reminderInput.Value())
	if input == "" {
		return time.Time{}, fmt.Errorf("enter a reminder time or choose a quick pick")
	}
	return when.Parse(input, time.Now())
}

func (m Model) reminderPreview() (time.Time, error) {
	if strings.TrimSpace(m.reminderInput.Value()) == "" {
		return time.Time{}, nil
	}
	return when.Parse(m.reminderInput.Value(), time.Now())
}

func (m *Model) setReminderChoice(index int) {
	choices := reminderChoices()
	if index < 0 || index >= len(choices) {
		return
	}
	m.reminderQuickPick = index
	m.reminderShortcut = ""
	m.reminderInput.SetValue(choices[index].input)
	m.reminderError = ""
}

func (m Model) updateReminder(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	choices := reminderChoices()
	switch key {
	case "esc":
		m.reminderActive = false
		m.reminderTargets = nil
		m.reminderQuickPick = -1
		m.reminderShortcut = ""
		m.reminderError = ""
		return m, nil
	case "enter":
		at, err := m.reminderTime()
		if err != nil {
			m.reminderError = err.Error()
			return m, nil
		}
		m.reminderActive = false
		m.reminderQuickPick = -1
		m.reminderShortcut = ""
		m.reminderError = ""
		targets := m.reminderTargets
		m.reminderTargets = nil
		return m, m.optimisticAct(targets, "Reminding", func() tea.Cmd {
			return m.setRemindersCmd(targets, at)
		})
	case "j", "down":
		if strings.TrimSpace(m.reminderInput.Value()) == "" || m.reminderQuickPick >= 0 {
			index := m.reminderQuickPick + 1
			if index >= len(choices) {
				index = 0
			}
			m.setReminderChoice(index)
			return m, nil
		}
	case "k", "up":
		if strings.TrimSpace(m.reminderInput.Value()) == "" || m.reminderQuickPick >= 0 {
			index := m.reminderQuickPick - 1
			if index < 0 {
				index = len(choices) - 1
			}
			m.setReminderChoice(index)
			return m, nil
		}
	}
	if len(key) == 1 && key >= "1" && key <= "6" && (strings.TrimSpace(m.reminderInput.Value()) == "" || m.reminderQuickPick >= 0) {
		m.setReminderChoice(int(key[0] - '1'))
		m.reminderShortcut = key
		return m, nil
	}

	// Any typed character switches back from a quick pick to the free-text
	// field. Keeping j/k available while the field is empty makes the picker
	// keyboard-friendly without preventing phrases such as "next monday".
	if m.reminderQuickPick >= 0 {
		// Numeric quick-pick shortcuts share the same keys as clock/date
		// expressions. Keep the choice when the user presses Enter, but if a
		// second character arrives reinterpret the first digit as free text
		// (17:30, 2026-09-12, 2pm, ...).
		seed := ""
		if m.reminderShortcut != "" && len(key) == 1 && ((key[0] >= '0' && key[0] <= '9') || key == ":" || key == "-" || key == "a" || key == "p") {
			seed = m.reminderShortcut
		}
		m.reminderInput.SetValue(seed)
	}
	m.reminderQuickPick = -1
	m.reminderShortcut = ""
	m.reminderError = ""
	var cmd tea.Cmd
	m.reminderInput, cmd = m.reminderInput.Update(msg)
	return m, cmd
}

func (m Model) viewReminder() string {
	choices := reminderChoices()
	title := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("Remind me in ...")
	input := styleInputLabel.Render("When:") + " " + m.reminderInput.View()
	lines := []string{title, "", input}
	if at, err := m.reminderPreview(); err == nil && !at.IsZero() {
		lines = append(lines, styleSuccess.Render("→ "+at.Local().Format("Mon, Jan 2 at 15:04 MST")))
	} else if err != nil {
		lines = append(lines, styleError.Render("✗ "+err.Error()))
	} else {
		lines = append(lines, styleHelp.Render("Type a time or choose a quick pick"))
	}
	lines = append(lines, "", styleHelp.Render("Quick picks"))
	for i, choice := range choices {
		style := lipgloss.NewStyle()
		if i == m.reminderQuickPick {
			style = style.Background(lipgloss.Color("#2D4F67"))
		}
		lines = append(lines, style.Render(fmt.Sprintf("  %d  %s", i+1, choice.label)))
	}
	if m.reminderError != "" {
		lines = append(lines, "", styleError.Render("✗ "+m.reminderError))
	}
	lines = append(lines, "", styleHelp.Render("j/k choose · 1-6 quick pick · enter set · esc cancel"))
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(1, 2).Width(62).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
