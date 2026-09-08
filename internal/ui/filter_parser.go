package ui

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sspaeti/neomd/internal/imap"
	"github.com/sspaeti/neomd/internal/when"
)

// ── Filter query parser (/) ─────────────────────────────────────────────
//
// The local filter accepts field tokens combined with free text:
//
//	from:simon        — From header contains "simon"
//	to:team@          — To/Cc/Bcc headers contain "team@"
//	subject:invoice   — Subject contains "invoice"
//	has:attachment    — only emails with attachments
//	before:yesterday  — Date strictly before the parsed time
//	after:"last week" — Date strictly after the parsed time (quoted
//	                    values may contain spaces)
//	in:trash          — folder matches the label/path/alias ("done",
//	                    "other", "all" included)
//
// Everything else is free text matched against From/Subject (+ contact
// names) as before. Tokens AND together; multiple tokens of the same kind
// AND as well (the last wins for single-value fields, matching grep-style
// expectations loosely but predictably).

// filterQuery is a parsed / filter expression.
type filterQuery struct {
	freeText      string
	from          string
	to            string
	subject       string
	hasAttachment bool
	before        time.Time
	after         time.Time
	inFolder      string // resolved IMAP folder path ("" = unset)
}

// hasFieldConstraints reports whether any field token was parsed. Free-text
// matching is handled separately (it needs contact names and Sent-folder
// context), everything here is a pure predicate on the email.
func (q filterQuery) hasFieldConstraints() bool {
	return q.from != "" || q.to != "" || q.subject != "" ||
		q.hasAttachment || !q.before.IsZero() || !q.after.IsZero() || q.inFolder != ""
}

// filterPrefixes are the recognized `key:` tokens (has:/in: handled specially).
var filterPrefixes = []string{"from:", "to:", "subject:", "before:", "after:", "in:"}

// splitFilterTokens splits a filter string on spaces, honouring double
// quotes so `after:"3 days ago"` stays one token.
func splitFilterTokens(s string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// parseFilterTime resolves a natural-language date for before:/after:.
// Unlike when.Parse (built for scheduling), past dates are valid here:
// absolute ISO dates (2026-03-10 [HH:MM]), a few relative phrases
// (yesterday, today, tomorrow, last week, "3 days ago", "2 weeks ago"),
// then when.Parse as fallback (weekdays, clock times). Ok is false when
// the value is empty or unparsable — the criterion is then dropped.
func parseFilterTime(value string, now time.Time) (time.Time, bool) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return time.Time{}, false
	}
	if t, ok := parseFilterDate(v, now); ok {
		return t, true
	}
	t, err := when.Parse(v, now)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

var filterDaysAgoRe = regexp.MustCompile(`^(\d+)\s+(day|week)s?\s+ago$`)

// parseFilterDate handles the filter-specific date forms without when's
// future-only restriction. Dates resolve to midnight (local) unless an
// explicit HH:MM is given, so "before:2026-03-10" means "strictly before
// that day starts".
func parseFilterDate(v string, now time.Time) (time.Time, bool) {
	midnight := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
	switch v {
	case "today":
		return midnight(now), true
	case "yesterday":
		return midnight(now.AddDate(0, 0, -1)), true
	case "tomorrow":
		return midnight(now.AddDate(0, 0, 1)), true
	case "last week":
		return midnight(now.AddDate(0, 0, -7)), true
	}
	if m := filterDaysAgoRe.FindStringSubmatch(v); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil && n > 0 {
			days := n
			if m[2] == "week" {
				days = n * 7
			}
			return midnight(now.AddDate(0, 0, -days)), true
		}
	}
	// Absolute ISO date, optional HH:MM.
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, now.Location()); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseFilterQuery parses a filter expression. now anchors relative dates
// ("yesterday", "3 days ago").
func parseFilterQuery(text string, now time.Time, folderAliases map[string]string) filterQuery {
	var q filterQuery
	var free []string
	for _, tok := range splitFilterTokens(strings.TrimSpace(text)) {
		lower := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(lower, "from:"):
			q.from = tok[len("from:"):]
		case strings.HasPrefix(lower, "to:"):
			q.to = tok[len("to:"):]
		case strings.HasPrefix(lower, "subject:"):
			q.subject = tok[len("subject:"):]
		case lower == "has:attachment" || lower == "has:attachments":
			q.hasAttachment = true
		case strings.HasPrefix(lower, "before:"):
			if t, ok := parseFilterTime(tok[len("before:"):], now); ok {
				q.before = t
			}
		case strings.HasPrefix(lower, "after:"):
			if t, ok := parseFilterTime(tok[len("after:"):], now); ok {
				q.after = t
			}
		case strings.HasPrefix(lower, "in:"):
			q.inFolder = resolveFolderAlias(tok[len("in:"):], folderAliases)
		default:
			free = append(free, tok)
		}
	}
	q.freeText = strings.TrimSpace(strings.Join(free, " "))
	return q
}

// resolveFolderAlias maps an in:<value> token to an IMAP folder path.
// Matching is case-insensitive against labels (Inbox, Feed, ToScreen…),
// raw folder paths, and a few convenience aliases ("done"→Archive,
// "other"→ScreenedOut). "all" and "" match every folder (empty result =
// no constraint). Unknown values return the value lowercased so matching
// a non-configured folder never silently broadens the filter.
func resolveFolderAlias(value string, aliases map[string]string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" || v == "all" {
		return ""
	}
	if path, ok := aliases[v]; ok {
		return path
	}
	return v
}

// folderAliases builds the lowercase alias → IMAP path map used by in:.
// Nil-safe: a nil config yields an empty map (no in: constraints resolve).
func (m *Model) folderAliases() map[string]string {
	if m.cfg == nil {
		return map[string]string{}
	}
	f := m.cfg.Folders
	entries := map[string]string{
		f.Inbox: f.Inbox, f.Sent: f.Sent, f.Trash: f.Trash, f.Drafts: f.Drafts,
		f.ToScreen: f.ToScreen, f.Feed: f.Feed, f.PaperTrail: f.PaperTrail,
		f.ScreenedOut: f.ScreenedOut, f.Archive: f.Archive, f.Waiting: f.Waiting,
		f.Scheduled: f.Scheduled, f.Someday: f.Someday, f.Spam: f.Spam,
	}
	if f.Work != "" {
		entries[f.Work] = f.Work
	}
	// Convenience aliases (Superhuman vocabulary + lowercase labels).
	entries["done"] = f.Archive
	entries["other"] = f.ScreenedOut
	entries["reminders"] = f.Waiting
	out := make(map[string]string, len(entries))
	for path, target := range entries {
		if path == "" {
			continue
		}
		out[strings.ToLower(path)] = target
		// Labels may differ from paths (Gmail "[Gmail]/All Mail").
		if label := f.LabelFor(path); label != path {
			out[strings.ToLower(label)] = target
		}
	}
	return out
}

// matchesEmail applies the parsed constraints to an imap.Email. All
// constraints must pass (AND). Free text is NOT evaluated here.
func (q filterQuery) matchesEmail(e imap.Email) bool {
	if q.from != "" && !strings.Contains(strings.ToLower(e.From), strings.ToLower(q.from)) {
		return false
	}
	if q.to != "" {
		hay := strings.ToLower(e.To + " " + e.CC + " " + e.BCC)
		if !strings.Contains(hay, strings.ToLower(q.to)) {
			return false
		}
	}
	if q.subject != "" && !strings.Contains(strings.ToLower(e.Subject), strings.ToLower(q.subject)) {
		return false
	}
	if q.hasAttachment && !e.HasAttachment {
		return false
	}
	if !q.before.IsZero() && !e.Date.Before(q.before) {
		return false
	}
	if !q.after.IsZero() && !e.Date.After(q.after) {
		return false
	}
	if q.inFolder != "" && !strings.EqualFold(e.Folder, q.inFolder) {
		return false
	}
	return true
}
