package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/contacts"
)

// composeStep tracks which field is active in the compose form.
type composeStep int

const (
	stepTo      composeStep = iota
	stepCC                  // only reachable when extraVisible=true
	stepBCC                 // only reachable when extraVisible=true
	stepSubject             // after Subject is filled, launch editor
)

// composeModel holds state for the compose view.
type composeModel struct {
	to           textinput.Model
	cc           textinput.Model
	bcc          textinput.Model
	subject      textinput.Model
	step         composeStep
	extraVisible bool // ctrl+b toggles Cc+Bcc together; off by default
	fromPresend  bool // true when ctrl+b was pressed from the pre-send screen (CC-only edit)

	// Address autocomplete
	knownAddrs  []string        // all addresses from screener lists (set once)
	contacts    *contacts.Store // harvested + [contacts]-file names (nil-safe); matched by name AND address
	suggestions []string        // current matching suggestions
	suggestI    int             // selected suggestion index (-1 = none)
}

func newComposeModel() composeModel {
	to := textinput.New()
	to.Placeholder = "recipient@example.com"
	to.Focus()
	to.CharLimit = 256
	to.Width = 60
	to.Prompt = ""

	cc := textinput.New()
	cc.Placeholder = "cc@example.com (optional)"
	cc.CharLimit = 512
	cc.Width = 60
	cc.Prompt = ""

	bcc := textinput.New()
	bcc.Placeholder = "bcc@example.com (optional)"
	bcc.CharLimit = 512
	bcc.Width = 60
	bcc.Prompt = ""

	sub := textinput.New()
	sub.Placeholder = "Subject"
	sub.CharLimit = 256
	sub.Width = 60
	sub.Prompt = ""

	return composeModel{to: to, cc: cc, bcc: bcc, subject: sub, step: stepTo, suggestI: -1}
}

// reset clears all fields and refocuses on To. Preserves knownAddrs.
func (c *composeModel) reset() {
	c.to.Reset()
	c.cc.Reset()
	c.bcc.Reset()
	c.subject.Reset()
	c.step = stepTo
	c.extraVisible = false
	c.fromPresend = false
	c.to.Focus()
	c.cc.Blur()
	c.bcc.Blur()
	c.subject.Blur()
	c.suggestions = nil
	c.suggestI = -1
	// knownAddrs is intentionally preserved
}

// activeField returns the textinput currently being edited.
func (c *composeModel) activeField() *textinput.Model {
	switch c.step {
	case stepTo:
		return &c.to
	case stepCC:
		return &c.cc
	case stepBCC:
		return &c.bcc
	default:
		return &c.subject
	}
}

// suggestMatches reports whether a candidate matches the typed query:
// substring hits always match, and a fuzzy subsequence of the name counts
// too ("mm" → "Max Muster"). Single-character queries stay substring-only.
func suggestMatches(text, query string) bool {
	lt := strings.ToLower(text)
	if strings.Contains(lt, query) {
		return true
	}
	if len(query) < 2 {
		return false
	}
	_, ok := fuzzyScore(lt, query)
	return ok
}

// isAddrField returns true if the current step is an address field (To/Cc/Bcc).
func (c *composeModel) isAddrField() bool {
	return c.step == stepTo || c.step == stepCC || c.step == stepBCC
}

// updateSuggestions refreshes the suggestion list based on the current input.
// Candidates come from two sources: the contacts store (harvested names +
// [contacts] file), matched by display name OR address and suggested as
// "Name <addr>", and the screener lists (bare addresses), decorated with a
// known name when the store has one. Deduped by bare address, contacts first.
func (c *composeModel) updateSuggestions() {
	c.suggestI = -1
	c.suggestions = nil
	if !c.isAddrField() {
		return
	}
	// Get the last address being typed (after the last comma)
	input := c.activeField().Value()
	lastPart := input
	if i := strings.LastIndex(input, ","); i >= 0 {
		lastPart = strings.TrimSpace(input[i+1:])
	}
	if lastPart == "" {
		return
	}
	query := strings.ToLower(lastPart)
	seen := map[string]bool{}
	for _, e := range c.contacts.All() {
		if suggestMatches(e.Name, query) || suggestMatches(e.Addr, query) {
			c.suggestions = append(c.suggestions, e.Name+" <"+e.Addr+">")
			seen[strings.ToLower(e.Addr)] = true
		}
	}
	for _, addr := range c.knownAddrs {
		if seen[strings.ToLower(addr)] {
			continue
		}
		if name := c.contacts.Name(addr); name != "" {
			// Screener address with a known name: match on either, suggest decorated.
			if suggestMatches(name, query) || suggestMatches(addr, query) {
				c.suggestions = append(c.suggestions, name+" <"+addr+">")
				seen[strings.ToLower(addr)] = true
			}
			continue
		}
		if suggestMatches(addr, query) {
			c.suggestions = append(c.suggestions, addr)
			seen[strings.ToLower(addr)] = true
		}
	}
	// Sort: prefix matches first, then alphabetical
	sort.Slice(c.suggestions, func(i, j int) bool {
		ip := strings.HasPrefix(strings.ToLower(c.suggestions[i]), query)
		jp := strings.HasPrefix(strings.ToLower(c.suggestions[j]), query)
		if ip != jp {
			return ip
		}
		return c.suggestions[i] < c.suggestions[j]
	})
	if len(c.suggestions) > 8 {
		c.suggestions = c.suggestions[:8]
	}
}

// applySuggestion inserts the selected suggestion into the active field.
func (c *composeModel) applySuggestion() {
	if c.suggestI < 0 || c.suggestI >= len(c.suggestions) {
		return
	}
	field := c.activeField()
	input := field.Value()
	selected := c.suggestions[c.suggestI]

	// Replace the last partial address with the full one
	if i := strings.LastIndex(input, ","); i >= 0 {
		prefix := input[:i+1] + " "
		field.SetValue(prefix + selected)
	} else {
		field.SetValue(selected)
	}
	field.SetCursor(len(field.Value()))
	c.suggestions = nil
	c.suggestI = -1
}

// update handles key input for the compose form.
// Returns (updated model, cmd, launchEditor bool).
func (c composeModel) update(msg tea.Msg) (composeModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+b":
			c.extraVisible = !c.extraVisible
			if !c.extraVisible {
				c.cc.Reset()
				c.bcc.Reset()
				// If cursor was on CC or BCC, jump to Subject
				if c.step == stepCC || c.step == stepBCC {
					c.cc.Blur()
					c.bcc.Blur()
					c.step = stepSubject
					c.subject.Focus()
				}
			}
			return c, nil, false

		case "tab":
			// Tab with suggestions → accept suggestion
			if len(c.suggestions) > 0 && c.isAddrField() {
				if c.suggestI < 0 {
					c.suggestI = 0
				}
				c.applySuggestion()
				return c, nil, false
			}
			// Tab without suggestions → next field (same as enter)
			return c.advanceField()

		case "shift+tab":
			// Move to the previous field (To ← Cc ← Bcc ← Subject).
			return c.retreatField()

		case "enter":
			// Enter always advances to next field
			if len(c.suggestions) > 0 && c.suggestI >= 0 {
				c.applySuggestion()
			}
			return c.advanceField()

		case "down", "ctrl+n":
			if len(c.suggestions) > 0 {
				c.suggestI = (c.suggestI + 1) % len(c.suggestions)
				return c, nil, false
			}

		case "up", "ctrl+p":
			if len(c.suggestions) > 0 {
				if c.suggestI <= 0 {
					c.suggestI = len(c.suggestions) - 1
				} else {
					c.suggestI--
				}
				return c, nil, false
			}
		}
	}

	var cmd tea.Cmd
	switch c.step {
	case stepTo:
		c.to, cmd = c.to.Update(msg)
	case stepCC:
		c.cc, cmd = c.cc.Update(msg)
	case stepBCC:
		c.bcc, cmd = c.bcc.Update(msg)
	default:
		c.subject, cmd = c.subject.Update(msg)
	}
	c.updateSuggestions()
	return c, cmd, false
}

// advanceField moves to the next compose field. Returns (model, cmd, launchEditor).
func (c composeModel) advanceField() (composeModel, tea.Cmd, bool) {
	c.suggestions = nil
	c.suggestI = -1
	switch c.step {
	case stepTo:
		if c.extraVisible {
			c.step = stepCC
			c.to.Blur()
			c.cc.Focus()
		} else {
			c.step = stepSubject
			c.to.Blur()
			c.subject.Focus()
		}
		return c, nil, false
	case stepCC:
		c.step = stepBCC
		c.cc.Blur()
		c.bcc.Focus()
		return c, nil, false
	case stepBCC:
		if c.fromPresend {
			return c, nil, true // signal: done editing CC/BCC, return to pre-send
		}
		c.step = stepSubject
		c.bcc.Blur()
		c.subject.Focus()
		return c, nil, false
	case stepSubject:
		return c, nil, true
	}
	return c, nil, false
}

// retreatField moves to the previous compose field. Skips Cc/Bcc when hidden.
func (c composeModel) retreatField() (composeModel, tea.Cmd, bool) {
	c.suggestions = nil
	c.suggestI = -1
	switch c.step {
	case stepSubject:
		if c.extraVisible {
			c.step = stepBCC
			c.subject.Blur()
			c.bcc.Focus()
		} else {
			c.step = stepTo
			c.subject.Blur()
			c.to.Focus()
		}
	case stepBCC:
		c.step = stepCC
		c.bcc.Blur()
		c.cc.Focus()
	case stepCC:
		c.step = stepTo
		c.cc.Blur()
		c.to.Focus()
	case stepTo:
		// already at the first field — no-op
	}
	return c, nil, false
}

// view renders the compose header form.
func (c composeModel) view() string {
	toLabel := styleInputLabel.Render("To:")
	subLabel := styleInputLabel.Render("Subject:")

	toField := c.to.View()
	subField := c.subject.View()

	switch c.step {
	case stepTo:
		toField = styleInputField.Render(toField)
	default:
		subField = styleInputField.Render(subField)
	}

	out := toLabel + toField + "\n"

	if c.extraVisible {
		ccLabel := styleInputLabel.Render("Cc:")
		bccLabel := styleInputLabel.Render("Bcc:")
		ccField := c.cc.View()
		bccField := c.bcc.View()
		if c.step == stepCC {
			ccField = styleInputField.Render(ccField)
			subField = c.subject.View() // undo active styling on Subject
		}
		if c.step == stepBCC {
			bccField = styleInputField.Render(bccField)
			subField = c.subject.View()
		}
		out += ccLabel + ccField + "\n"
		out += bccLabel + bccField + "\n"
	} else {
		out += styleHelp.Render("  ctrl+b to add Cc/Bcc") + "\n"
	}

	out += subLabel + subField

	// Show autocomplete suggestions
	if len(c.suggestions) > 0 && c.isAddrField() {
		out += "\n"
		for i, s := range c.suggestions {
			if i == c.suggestI {
				out += styleSuggestionSelected.Render("  > "+s) + "\n"
			} else {
				out += styleSuggestion.Render("    "+s) + "\n"
			}
		}
		out += styleHelp.Render("  tab accept · ↑↓ select")
	}

	return out
}
