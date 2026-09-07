package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sspaeti/neomd/internal/link"
)

type copyTarget struct {
	key   string
	label string
	text  string
}

func (m Model) copyTargets() ([]copyTarget, error) {
	if m.openEmail == nil {
		return nil, fmt.Errorf("no email selected")
	}
	messageURI, err := link.MessageIDURI(m.openEmail.MessageID)
	if err != nil {
		return nil, err
	}
	targets := []copyTarget{{key: "m", label: "Message-ID link", text: messageURI}}
	if m.openWebURL != "" {
		targets = append(targets, copyTarget{key: "w", label: "Web-version link", text: m.openWebURL})
	}
	return targets, nil
}

func (m Model) openCopyMenu() (tea.Model, tea.Cmd) {
	targets, err := m.copyTargets()
	if err != nil {
		m.status = "Copy link: " + err.Error()
		m.isError = true
		return m, nil
	}
	m.prevState = m.state
	m.copyTargetsList = targets
	m.copyMenuCursor = 0
	m.state = stateCopyMenu
	m.status = ""
	m.isError = false
	return m, nil
}

func (m Model) updateCopyMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q":
		m.state = m.prevState
		m.copyTargetsList = nil
		return m, nil
	case "j", "down":
		if m.copyMenuCursor < len(m.copyTargetsList)-1 {
			m.copyMenuCursor++
		}
		return m, nil
	case "k", "up":
		if m.copyMenuCursor > 0 {
			m.copyMenuCursor--
		}
		return m, nil
	case "enter":
		if len(m.copyTargetsList) == 0 {
			return m, nil
		}
		return m.copyTargetCmd(m.copyTargetsList[m.copyMenuCursor])
	}
	for _, target := range m.copyTargetsList {
		if key == target.key {
			return m.copyTargetCmd(target)
		}
	}
	return m, nil
}

func (m Model) copyTargetCmd(target copyTarget) (tea.Model, tea.Cmd) {
	m.state = m.prevState
	m.copyTargetsList = nil
	m.status = "Copying " + target.label + "…"
	m.isError = false
	return m, copyToClipboardCmd(target.text)
}

func (m Model) viewCopyMenu() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7E9CD8")).Render("Copy from email")
	items := make([]string, 0, len(m.copyTargetsList))
	for i, target := range m.copyTargetsList {
		style := lipgloss.NewStyle()
		if i == m.copyMenuCursor {
			style = style.Background(lipgloss.Color("#2D4F67"))
		}
		items = append(items, style.Render(fmt.Sprintf("  %s  %s", target.key, target.label)))
	}
	help := styleHelp.Render("j/k move · enter or shortcut copy · esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", strings.Join(items, "\n"), "", help)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#54546D")).Padding(1, 2).Width(48).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
