package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"unicode"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/sspaeti/neomd/internal/imap"
)

// emailItem wraps imap.Email to satisfy bubbles/list.Item.
type emailItem struct {
	email        imap.Email
	index        int    // position in list (1-based)
	marked       bool   // selected for batch operation
	displaySubj  string // rendered subject (may include folder prefix in temporary views)
	threadPrefix string // tree chars e.g. "┌─>" for threaded display
	hasSpyPixel  bool   // tracking pixels were detected when body was loaded
	threadCount  int    // >0 when this row represents a collapsed thread (count of messages)
}

func (e emailItem) FilterValue() string {
	return e.email.From + " " + e.email.Subject
}

func (e emailItem) Title() string       { return e.email.Subject }
func (e emailItem) Description() string { return e.email.From }

// emailDelegate is a custom list.ItemDelegate that renders one email per row.
type emailDelegate struct {
	sentFolder  string // when active folder matches, show To instead of From
	draftFolder string // when active folder matches, show To instead of From
}

func (d emailDelegate) Height() int                             { return 1 }
func (d emailDelegate) Spacing() int                            { return 0 }
func (d emailDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// Column widths
const (
	colNumWidth      = 4 // "  1 "
	colFlagWidth     = 2 // "N " or "  "
	colReplyWidth    = 1 // "·" or " "
	colThreadWidth   = 2 // "│ " or "╰ " or "  "
	colDateWidth     = 7 // "Feb 03 "
	colAttachWidth   = 2 // "@ " or "  "
	colSpyWidth      = 2 // "°" or "  " — spy pixel indicator
	colReminderWidth = 2 // "R " or "  " — scheduled/due reminder
	colSizeWidth     = 7 // "(38.2K)"
)

func (d emailDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	e, ok := item.(emailItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()
	unread := !e.email.Seen

	width := m.Width()
	if width <= 0 {
		width = 80
	}

	// Fixed columns
	num := fmt.Sprintf("%3d ", e.index)
	// Flag column: mark takes priority; show unread alongside mark
	flag := "  "
	switch {
	case e.marked && !e.email.Seen:
		flag = "*N"
	case e.marked:
		flag = "* "
	case unread:
		flag = "N "
	}
	// Reply indicator
	replyStr := " "
	if e.email.Answered {
		replyStr = "·"
	}
	// Thread connector column; a collapsed thread shows ▸ with count badge.
	threadStr := "  "
	if e.threadCount > 0 {
		threadStr = "▸ "
	} else if e.threadPrefix != "" {
		threadStr = e.threadPrefix + " "
	}
	dateStr := fmtDate(e.email.Date) + " "
	attachStr := "  "
	if e.email.HasAttachment {
		attachStr = "@ "
	}
	spyStr := "  "
	if e.hasSpyPixel {
		spyStr = "° "
	}
	reminderStr := "  "
	if e.email.Reminder != nil {
		reminderStr = "R "
	}
	sizeStr := fmtSize(e.email.Size)

	fixed := colNumWidth + colFlagWidth + colReplyWidth + colThreadWidth + colDateWidth + colAttachWidth + colSpyWidth + colReminderWidth + colSizeWidth + 2 // 2 spaces padding
	fromMax := 20
	subjectMax := width - fixed - fromMax - 2
	if subjectMax < 8 {
		subjectMax = 8
	}

	sender := e.email.From
	if d.sentFolder != "" && e.email.Folder == d.sentFolder {
		sender = "→ " + e.email.To // show recipient in Sent
	} else if d.draftFolder != "" && e.email.Folder == d.draftFolder {
		sender = "→ " + e.email.To // show recipient in Drafts
	}
	from := truncate(displaySafe(cleanFrom(sender)), fromMax)
	subjectText := e.email.Subject
	if e.displaySubj != "" {
		subjectText = e.displaySubj
	}
	subjectText = sendLaterPrefix(e.email) + subjectText
	if e.threadCount > 0 {
		subjectText = fmt.Sprintf("×%d ", e.threadCount) + subjectText
	}
	if e.email.Reminder != nil {
		// Follow-up reminders stay visible in the list with their due time
		// (Waiting/Reminders view), not just via the R indicator.
		subjectText += "  ⏰ " + e.email.Reminder.At.Local().Format("Jan 2 15:04")
	}
	subject := truncate(displaySafe(subjectText), subjectMax)

	if isSelected {
		row := num + flag + replyStr + threadStr + dateStr + attachStr + spyStr + reminderStr +
			padRight(from, fromMax) + "  " +
			padRight(subject, subjectMax) + "  " +
			sizeStr
		fmt.Fprint(w, styleSelected.Render(row))
		return
	}

	// Colorise each column separately
	numS := lipgloss.NewStyle().Foreground(colorNumber).Render(num)
	var flagS string
	switch {
	case e.marked:
		flagS = lipgloss.NewStyle().Foreground(colorDateCol).Bold(true).Render(flag)
	case unread:
		flagS = lipgloss.NewStyle().Foreground(colorAuthorUnread).Bold(true).Render(flag)
	default:
		flagS = lipgloss.NewStyle().Foreground(colorMuted).Render(flag)
	}
	replyS := lipgloss.NewStyle().Foreground(colorMuted).Render(replyStr)
	if e.email.Answered {
		replyS = lipgloss.NewStyle().Foreground(colorPrimary).Render(replyStr)
	}
	threadS := lipgloss.NewStyle().Foreground(colorBorder).Render(threadStr)
	dateS := lipgloss.NewStyle().Foreground(colorDateCol).Render(dateStr)
	attachS := lipgloss.NewStyle().Foreground(colorDateCol).Render(attachStr)
	spyS := lipgloss.NewStyle().Foreground(colorMuted).Render(spyStr)
	if e.hasSpyPixel {
		spyS = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render(spyStr) // orange warning
	}
	reminderS := lipgloss.NewStyle().Foreground(colorMuted).Render(reminderStr)
	if e.email.Reminder != nil {
		reminderS = lipgloss.NewStyle().Foreground(colorPrimary).Render(reminderStr)
	}

	fromStyle := lipgloss.NewStyle().Foreground(colorAuthorRead)
	subStyle := lipgloss.NewStyle().Foreground(colorSubjectRead)
	if unread {
		fromStyle = lipgloss.NewStyle().Foreground(colorAuthorUnread).Bold(true)
		subStyle = lipgloss.NewStyle().Foreground(colorSubjectUnread).Bold(true)
	}
	fromS := fromStyle.Render(padRight(from, fromMax))
	subS := subStyle.Render(padRight(subject, subjectMax))
	sizeS := lipgloss.NewStyle().Foreground(colorSizeCol).Render(sizeStr)

	fmt.Fprint(w, numS+flagS+replyS+threadS+dateS+attachS+spyS+reminderS+fromS+"  "+subS+"  "+sizeS)
}

// cleanFrom strips the <addr> part when a display name is present.
func cleanFrom(from string) string {
	if i := strings.Index(from, " <"); i > 0 {
		return from[:i]
	}
	return from
}

// fmtSize formats a byte count into a compact "(38.2K)" string like neomutt.
func fmtSize(b uint32) string {
	switch {
	case b == 0:
		return "       "
	case b < 1024:
		return fmt.Sprintf("(%4dB)", b)
	case b < 1024*1024:
		return fmt.Sprintf("(%4.0fK)", float64(b)/1024)
	default:
		return fmt.Sprintf("(%4.1fM)", float64(b)/(1024*1024))
	}
}

// fmtDateFull returns a date+time string for the reader header (always
// includes the clock time, unlike fmtDate which is compact for list rows).
func fmtDateFull(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	if t.Year() == time.Now().Year() {
		return t.Format("Jan 02, 15:04")
	}
	return t.Format("Jan 02 2006, 15:04")
}

func fmtDate(t time.Time) string {
	if t.IsZero() {
		return "      "
	}
	t = t.Local()
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04 ")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 02")
	}
	return t.Format("Jan 06")
}

// sendLaterPrefix returns the display-only "[send-later …]" marker for
// messages queued by the send-later feature (identified by their
// X-Neomd-Send-At header). The stored subject is never mutated — it is what
// gets delivered — so regular GTD mail sharing the Scheduled folder stays
// visually distinct without any wire change.
func sendLaterPrefix(e imap.Email) string {
	if e.SendAt.IsZero() {
		return ""
	}
	return "[send-later " + e.SendAt.Local().Format("Jan 2 15:04") + "] "
}

// displaySafe collapses every run of characters from scripts whose
// terminal-cell-width is unpredictable (Bengali, Arabic, Thai, emoji,
// combining marks, variation selectors …) into a single '·' placeholder.
// Latin-derived scripts, common punctuation, and CJK (Hangul, Han, kana)
// pass through unchanged — terminals and runewidth agree those are exactly
// 2 cells per character, so the row width stays controllable.
//
// Why so blunt for the unsafe runs: terminal emulators and width libraries
// disagree on how many cells a Bengali / Devanagari / Thai grapheme cluster
// occupies (see the "Grapheme Clusters and Terminal Emulators" write-up
// and lipgloss #562). Anything we measure with runewidth/uniseg can render
// wider in foot or kitty, overflowing the row and making the bubbles list
// lose its top when the cursor moves. Replacing the unpredictable runs
// with a single 1-cell character gives every row a width we can actually
// control, at the cost of not showing the original glyphs in the list.
// The reader view applies the same transform to the subject line so the
// rounded-border header box stays aligned.
func displaySafe(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inMarker := false
	for _, r := range s {
		// Zero-width chars (combining marks, variation selectors, format chars)
		// are silently dropped — keeps "werden︅" rendering as "werden" instead
		// of "werden·". Doesn't reset inMarker so they also don't visually
		// re-open a placeholder inside an already-collapsed unsafe run.
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if safeForDisplay(r) {
			b.WriteRune(r)
			inMarker = false
			continue
		}
		if !inMarker {
			b.WriteRune('·')
			inMarker = true
		}
	}
	return b.String()
}

// safeForDisplay reports whether r is safe to render directly in a width-
// constrained TUI column. Latin scripts, Greek, Cyrillic, common
// punctuation, and CJK (Hangul, Han, kana — uniformly East Asian Wide,
// 2 cells) are safe; complex scripts (Bengali, Devanagari, Thai, Arabic),
// emoji, combining marks and variation selectors are not.
func safeForDisplay(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return true
	case r < 0x20 || r == 0x7F:
		return false
	case r < 0x80:
		return true
	case r >= 0xA0 && r <= 0x024F:
		// Latin-1 Supplement + Latin Extended A/B (umlauts, ß, accented letters)
		return true
	case r >= 0x1E00 && r <= 0x1EFF:
		// Latin Extended Additional
		return true
	case r >= 0x0370 && r <= 0x03FF:
		// Greek
		return true
	case r >= 0x0400 && r <= 0x04FF:
		// Cyrillic
		return true
	case r >= 0x2010 && r <= 0x205E:
		// General punctuation (en/em dash, quotes, ellipsis, …)
		return true
	case r >= 0x20A0 && r <= 0x20CF:
		// Currency symbols
		return true
	case r >= 0x1100 && r <= 0x11FF:
		// Hangul Jamo
		return true
	case r >= 0x3000 && r <= 0x303F:
		// CJK Symbols and Punctuation
		return true
	case r >= 0x3040 && r <= 0x309F:
		// Hiragana
		return true
	case r >= 0x30A0 && r <= 0x30FF:
		// Katakana
		return true
	case r >= 0x3130 && r <= 0x318F:
		// Hangul Compatibility Jamo
		return true
	case r >= 0x3400 && r <= 0x4DBF:
		// CJK Unified Ideographs Extension A
		return true
	case r >= 0x4E00 && r <= 0x9FFF:
		// CJK Unified Ideographs
		return true
	case r >= 0xA960 && r <= 0xA97F:
		// Hangul Jamo Extended-A
		return true
	case r >= 0xAC00 && r <= 0xD7A3:
		// Hangul Syllables
		return true
	case r >= 0xD7B0 && r <= 0xD7FF:
		// Hangul Jamo Extended-B
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		// CJK Compatibility Ideographs
		return true
	case r >= 0xFF00 && r <= 0xFFEF:
		// Halfwidth and Fullwidth Forms
		return true
	}
	return false
}

// truncate shortens s to fit within max terminal cells, appending "…" when
// truncated. Assumes the caller has passed s through displaySafe so every
// rune has predictable cell width.
func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	return runewidth.Truncate(s, max, "…")
}

// padRight right-pads s with spaces so its display width equals w cells.
// Assumes s has been passed through displaySafe.
func padRight(s string, w int) string {
	pad := w - runewidth.StringWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// newInboxList creates a bubbles/list configured for the email inbox.
// sentFolder/draftFolder are IMAP folder names — used to show To instead of From.
func newInboxList(width, height int, sentFolder, draftFolder string) list.Model {
	l := list.New(nil, emailDelegate{sentFolder: sentFolder, draftFolder: draftFolder}, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false) // we manage filtering ourselves (filterText in Model)
	l.DisableQuitKeybindings()
	l.Styles.NoItems = styleStatus
	return l
}

// setEmails replaces the list contents, preserving marked state.
// It threads emails before display — grouped conversations appear together
// with tree-drawing prefixes (┌─>) on reply rows.
// Sorting respects the user's chosen sortField and sortReverse preferences.
// spyPixelKey returns a unique cache key for spy pixel tracking across folders.
func spyPixelKey(folder string, uid uint32) string {
	return folder + "\x00" + fmt.Sprintf("%d", uid)
}

func setEmails(l *list.Model, emails []imap.Email, marked map[uint32]bool, spyPixels map[string]bool, prefixFolders bool, sortField string, sortReverse bool, disableThreading bool, collapseThreads bool, expandedThreads map[string]bool) tea.Cmd {
	var threaded []threadedEmail
	if disableThreading {
		threaded = flatEmails(emails, sortField, sortReverse)
	} else {
		threaded = threadEmails(emails, sortField, sortReverse)
	}
	if collapseThreads {
		threaded = collapseThreadRows(threaded, expandedThreads)
	}
	items := make([]list.Item, len(threaded))
	for i, te := range threaded {
		displaySubj := te.email.Subject
		if prefixFolders {
			displaySubj = "[" + te.email.Folder + "] " + displaySubj
		}
		items[i] = emailItem{
			email:        te.email,
			index:        i + 1,
			marked:       marked[te.email.UID],
			displaySubj:  displaySubj,
			threadPrefix: te.threadPrefix,
			hasSpyPixel:  spyPixels[spyPixelKey(te.email.Folder, te.email.UID)],
			threadCount:  te.threadCount,
		}
	}
	return l.SetItems(items)
}

// collapseThreadRows reduces each multi-message thread to its newest row,
// carrying the message count in threadCount. Threads whose key is in the
// expanded set keep all rows. Single-message rows pass through untouched.
func collapseThreadRows(threaded []threadedEmail, expanded map[string]bool) []threadedEmail {
	if len(threaded) == 0 {
		return threaded
	}
	out := make([]threadedEmail, 0, len(threaded))
	for start := 0; start < len(threaded); {
		end := start
		for end+1 < len(threaded) && threaded[end+1].threadPrefix != "" && threaded[start].threadPrefix != "" {
			end++
		}
		block := threaded[start : end+1]
		if len(block) == 1 {
			out = append(out, block[0])
		} else {
			key := threadKey(block[0].email)
			if expanded[key] {
				out = append(out, block...)
			} else {
				top := block[0]
				top.threadCount = len(block)
				top.threadPrefix = ""
				out = append(out, top)
			}
		}
		start = end + 1
	}
	return out
}

// selectedEmailItem returns the currently highlighted email item, or the
// zero value with ok=false (used when collapsed-thread state matters).
func selectedEmailItem(l list.Model) (emailItem, bool) {
	item, ok := l.SelectedItem().(emailItem)
	return item, ok
}

// selectedEmail returns the currently highlighted email, or nil.
func selectedEmail(l list.Model) *imap.Email {
	item, ok := l.SelectedItem().(emailItem)
	if !ok {
		return nil
	}
	e := item.email
	return &e
}
