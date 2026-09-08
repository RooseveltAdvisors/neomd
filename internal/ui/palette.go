package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/snippets"
	"github.com/sspaeti/neomd/internal/when"
)

// ── Fuzzy command palette (:) ───────────────────────────────────────────
//
// The command line matches commands prefix-first; when nothing matches by
// prefix it falls back to fuzzy subsequence matching (fzf-style), ranked by
// score. Commands may take an argument (`:remind tomorrow 9am`), split off
// before matching. While typing, the preview line shows exactly what the
// command will do before it runs.

// fuzzyScore returns the best subsequence match score of query against
// text, or ok=false when query is not a subsequence. Lower is better:
// contiguous runs and word-boundary hits rank ahead of scattered ones.
func fuzzyScore(text, query string) (score int, ok bool) {
	t := strings.ToLower(text)
	q := strings.ToLower(query)
	if q == "" {
		return 0, true
	}
	ti := 0
	last := -1
	gaps := 0
	for _, qr := range q {
		found := false
		for ; ti < len(t); ti++ {
			if rune(t[ti]) == qr {
				if last >= 0 {
					gaps += ti - last - 1
				}
				last = ti
				ti++
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
	}
	return gaps, true
}

// matchCmdsFuzzy returns matching commands for the query: prefix matches
// win exclusively; fuzzy subsequence matches are only considered when
// nothing matches by prefix (ranked by score). Empty text returns all.
func matchCmdsFuzzy(text string) []*neomdCmd {
	lower := strings.ToLower(text)
	var prefix, fuzzy []*neomdCmd
	fuzzyScores := map[*neomdCmd]int{}
	for i := range cmdRegistry {
		c := &cmdRegistry[i]
		if lower == "" || strings.HasPrefix(c.name, lower) {
			prefix = append(prefix, c)
			continue
		}
		matchedAlias := false
		for _, a := range c.aliases {
			if strings.HasPrefix(a, lower) {
				matchedAlias = true
				break
			}
		}
		if matchedAlias {
			prefix = append(prefix, c)
			continue
		}
		if len(lower) < 2 {
			continue // single chars: prefixes only, no fuzzy surprises
		}
		if s, ok := fuzzyScore(c.name, lower); ok {
			fuzzy = append(fuzzy, c)
			fuzzyScores[c] = s
			continue
		}
		for _, a := range c.aliases {
			if s, ok := fuzzyScore(a, lower); ok {
				fuzzy = append(fuzzy, c)
				fuzzyScores[c] = s
				break
			}
		}
	}
	if len(prefix) > 0 {
		return prefix
	}
	// Rank fuzzy matches by score (stable for equal scores).
	for i := 1; i < len(fuzzy); i++ {
		for j := i; j > 0 && fuzzyScores[fuzzy[j]] < fuzzyScores[fuzzy[j-1]]; j-- {
			fuzzy[j], fuzzy[j-1] = fuzzy[j-1], fuzzy[j]
		}
	}
	return fuzzy
}

// matchCmdLine splits a command-line input into command + argument and
// resolves the command with fuzzy matching. Only the first word selects
// the command; the remainder is its argument.
func matchCmdLine(input string) (*neomdCmd, string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, ""
	}
	head, arg := input, ""
	if i := strings.IndexAny(input, " \t"); i >= 0 {
		head, arg = input[:i], strings.TrimSpace(input[i+1:])
	}
	if m := matchCmdsFuzzy(head); len(m) > 0 {
		return m[0], arg
	}
	return nil, ""
}

// cmdLinePreview renders the live preview for the current input: what the
// resolved command will do, with real counts and destinations where known.
func (m *Model) cmdLinePreview(input string) string {
	cmd, arg := matchCmdLine(input)
	if cmd == nil {
		return ""
	}
	if cmd.preview != nil {
		if p := cmd.preview(m, arg); p != "" {
			return cmd.name + " " + arg + " — " + p
		}
	}
	if arg != "" {
		return cmd.name + " " + arg + " — " + cmd.desc
	}
	return cmd.name + " — " + cmd.desc
}

// selectedTargets summarizes what the cursor/marked selection would act on.
func targetSummary(m *Model) string {
	targets := m.targetEmails()
	switch len(targets) {
	case 0:
		return "nothing selected"
	case 1:
		return "1 email (" + cleanFrom(targets[0].From) + ")"
	default:
		return fmt.Sprintf("%d emails", len(targets))
	}
}

// ── Argument commands ────────────────────────────────────────────────────

// runDoneCmd archives the selected/marked emails (same move as `e`).
func runDoneCmd(m *Model) (tea.Model, tea.Cmd) {
	return runMoveCmd(m, m.cfg.Folders.Archive)
}

// runMoveCmd moves the selected/marked emails to dst via the optimistic path.
func runMoveCmd(m *Model, dst string) (tea.Model, tea.Cmd) {
	targets := m.targetEmails()
	if len(targets) == 0 {
		m.status = "Nothing selected."
		m.isError = true
		return m, nil
	}
	cmd := m.optimisticAct(targets, "Moving", func() tea.Cmd {
		return m.batchMoveCmd(targets, dst)
	})
	return m, cmd
}

// runRemindCmd sets a reminder for the selected emails at a natural-language
// time (`:remind tomorrow 9am`, `:remind in 3 days`).
func runRemindCmd(m *Model, arg string) (tea.Model, tea.Cmd) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		// No argument: open the interactive reminder prompt (h).
		targets := m.targetEmails()
		if len(targets) == 0 {
			m.status = "Nothing selected."
			m.isError = true
			return m, nil
		}
		m.reminderTargets = targets
		m.reminderInput = newReminderInput()
		m.reminderQuickPick = -1
		m.reminderShortcut = ""
		m.reminderError = ""
		m.reminderActive = true
		return m, nil
	}
	at, err := when.Parse(arg, time.Now())
	if err != nil {
		m.status = "remind: " + err.Error()
		m.isError = true
		return m, nil
	}
	targets := m.targetEmails()
	if len(targets) == 0 {
		m.status = "Nothing selected."
		m.isError = true
		return m, nil
	}
	cmd := m.optimisticAct(targets, "Reminding", func() tea.Cmd {
		return m.setRemindersCmd(targets, at)
	})
	return m, cmd
}

// runMoveCmdArg resolves `:move <folder>` (label, path or alias) and moves
// the selection there. Without an argument it opens the interactive folder
// picker.
func runMoveCmdArg(m *Model, arg string) (tea.Model, tea.Cmd) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return m.openMovePicker()
	}
	aliases := m.folderAliases()
	dst, ok := aliases[strings.ToLower(arg)]
	if !ok {
		m.status = "move: unknown folder " + arg
		m.isError = true
		return m, nil
	}
	return runMoveCmd(m, dst)
}

// runSnipCmd opens the snippet manager (`:snip`) or composes from a named
// snippet (`:snip standup`, fuzzy-matched).
func runSnipCmd(m *Model, arg string) (tea.Model, tea.Cmd) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return m.openSnippetManager()
	}
	snips := m.snippets
	if len(snips) == 0 {
		snips = m.reloadSnippets()
	}
	best, ok := bestSnippetMatch(snips, arg)
	if !ok {
		m.status = "snip: no snippet matches " + arg
		m.isError = true
		return m, nil
	}
	return m.composeFromSnippet(*best)
}

// bestSnippetMatch fuzzy-matches a snippet by name (prefix beats subsequence).
func bestSnippetMatch(snips []snippets.Snippet, query string) (*snippets.Snippet, bool) {
	lower := strings.ToLower(query)
	var best *snippets.Snippet
	bestScore := -1
	for i := range snips {
		s := &snips[i]
		name := strings.ToLower(s.Name)
		var score int
		switch {
		case name == lower:
			score = 0
		case strings.HasPrefix(name, lower):
			score = 1
		default:
			fs, ok := fuzzyScore(name, lower)
			if !ok {
				continue
			}
			score = 10 + fs
		}
		if bestScore == -1 || score < bestScore {
			best, bestScore = s, score
		}
	}
	return best, best != nil
}

// preview helpers keep the live preview concrete: they describe the exact
// effect (destination folder, parsed time, snippet name) before running.

func previewDone(m *Model, _ string) string {
	return "archive " + targetSummary(m) + " → " + m.cfg.Folders.Archive
}

func previewRemind(m *Model, arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "choose a time for " + targetSummary(m)
	}
	at, err := when.Parse(arg, time.Now())
	if err != nil {
		return "invalid time: " + err.Error()
	}
	return fmt.Sprintf("remind %s at %s", targetSummary(m), at.Local().Format("Mon 2006-01-02 15:04"))
}

func previewMove(m *Model, arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "pick a folder for " + targetSummary(m)
	}
	dst, ok := m.folderAliases()[strings.ToLower(arg)]
	if !ok {
		return "unknown folder " + arg
	}
	return "move " + targetSummary(m) + " → " + dst
}

func previewSnip(m *Model, arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "open the snippet manager"
	}
	snips := m.snippets
	if len(snips) == 0 {
		snips = m.reloadSnippets()
	}
	if s, ok := bestSnippetMatch(snips, arg); ok {
		return "compose from \"" + s.Name + "\""
	}
	return "no snippet matches " + arg
}
