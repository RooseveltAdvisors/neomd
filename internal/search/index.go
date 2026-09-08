// Package search contains the small, private full-text index used by the TUI.
//
// The index deliberately keeps only decoded searchable text and message
// identity metadata. Attachments are never retained or searched. IMAP remains
// the source of truth; callers refresh headers and only fetch bodies whose
// fingerprint is new or changed.
package search

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sspaeti/neomd/internal/imap"
)

type document struct {
	email       imap.Email
	contactText string
	body        string
	bodyReady   bool
	fingerprint string
	scope       string
}

// Index is safe to refresh and query from separate Bubble Tea commands.
type Index struct {
	mu   sync.RWMutex
	docs map[string]document
}

// New returns an empty full-text index.
func New() *Index { return &Index{docs: make(map[string]document)} }

// Key identifies a message without exposing mailbox contents.
func Key(e imap.Email) string {
	return e.Account + "\x00" + e.Folder + "\x00" + strconv.FormatUint(uint64(e.UID), 10)
}

func scope(e imap.Email) string { return e.Account + "\x00" + e.Folder }

func fingerprint(e imap.Email) string {
	return strings.Join([]string{e.Subject, e.From, e.To, e.CC, e.BCC,
		e.Date.UTC().Format(time.RFC3339Nano), strconv.FormatUint(uint64(e.Size), 10)}, "\x00")
}

// NeedsBody reports whether the decoded body is absent or stale.
func (i *Index) NeedsBody(e imap.Email) bool {
	i.mu.RLock()
	d, ok := i.docs[Key(e)]
	i.mu.RUnlock()
	return !ok || !d.bodyReady || d.fingerprint != fingerprint(e)
}

// UpsertHeader records fresh envelope metadata while retaining a valid body.
func (i *Index) UpsertHeader(e imap.Email, contactText string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	k := Key(e)
	d := i.docs[k]
	if d.bodyReady && d.fingerprint == fingerprint(e) {
		d.email, d.contactText, d.scope = e, contactText, scope(e)
		i.docs[k] = d
		return
	}
	i.docs[k] = document{email: e, contactText: contactText, fingerprint: fingerprint(e), scope: scope(e), bodyReady: false}
}

// UpsertBody stores decoded plain/HTML text for a message.
func (i *Index) UpsertBody(e imap.Email, contactText, plain, html string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.docs[Key(e)] = document{email: e, contactText: contactText, body: plain + "\n" + stripHTML(html), bodyReady: true, fingerprint: fingerprint(e), scope: scope(e)}
}

// RemoveMissing removes messages from successful scopes that disappeared
// since the last refresh. Failed scopes are intentionally left untouched.
func (i *Index) RemoveMissing(present map[string]struct{}, successfulScopes map[string]struct{}) {
	i.mu.Lock()
	defer i.mu.Unlock()
	for k, d := range i.docs {
		if _, ok := successfulScopes[d.scope]; ok {
			if _, present := present[k]; !present {
				delete(i.docs, k)
			}
		}
	}
}

// Stats reports index state without revealing message content.
type Stats struct {
	Documents int
	Bodies    int
}

func (i *Index) Stats() Stats {
	i.mu.RLock()
	defer i.mu.RUnlock()
	s := Stats{Documents: len(i.docs)}
	for _, d := range i.docs {
		if d.bodyReady {
			s.Bodies++
		}
	}
	return s
}

type query struct {
	from, to, subject []string
	free              []string
}

func parseQuery(text string) query {
	var q query
	for _, token := range queryTokens(text) {
		lower := strings.ToLower(token)
		switch {
		case strings.HasPrefix(lower, "from:"):
			q.from = append(q.from, tokenize(token[5:])...)
		case strings.HasPrefix(lower, "to:"):
			q.to = append(q.to, tokenize(token[3:])...)
		case strings.HasPrefix(lower, "subject:"):
			q.subject = append(q.subject, tokenize(token[8:])...)
		case strings.HasPrefix(lower, "before:"), strings.HasPrefix(lower, "after:"), strings.HasPrefix(lower, "in:"), strings.HasPrefix(lower, "has:"):
			// Metadata constraints are applied by the UI after text retrieval.
		default:
			q.free = append(q.free, tokenize(token)...)
		}
	}
	return q
}

// Search returns matching emails in relevance/date order. Text matching is
// case-insensitive, accepts prefixes, and tolerates one edit for terms of
// four or more letters. All terms in a query must match.
func (i *Index) Search(text string) []imap.Email {
	q := parseQuery(text)
	i.mu.RLock()
	defer i.mu.RUnlock()
	type scored struct {
		e     imap.Email
		score int
	}
	var hits []scored
	for _, d := range i.docs {
		score := 0
		if !matchTerms(q.from, tokenize(d.email.From+" "+d.contactText), &score, 8) ||
			!matchTerms(q.to, tokenize(d.email.To+" "+d.email.CC+" "+d.email.BCC+" "+d.contactText), &score, 7) ||
			!matchTerms(q.subject, tokenize(d.email.Subject), &score, 10) ||
			!matchTerms(q.free, tokenize(d.email.From+" "+d.email.To+" "+d.email.CC+" "+d.email.BCC+" "+d.email.Subject+" "+d.contactText+" "+d.body), &score, 3) {
			continue
		}
		hits = append(hits, scored{e: d.email, score: score})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return hits[a].e.Date.After(hits[b].e.Date)
	})
	out := make([]imap.Email, len(hits))
	for n := range hits {
		out[n] = hits[n].e
	}
	return out
}

func matchTerms(terms, field []string, score *int, weight int) bool {
	for _, term := range terms {
		matched := false
		for _, candidate := range field {
			if candidate == term {
				*score += weight + 3
				matched = true
				break
			}
			if strings.HasPrefix(candidate, term) {
				*score += weight + 1
				matched = true
				break
			}
			if len([]rune(term)) >= 4 && levenshteinAtMostOne(term, candidate) {
				*score += weight
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, strings.ToLower(b.String()))
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '@' || r == '.' || r == '-' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func queryTokens(s string) []string {
	var out []string
	var b strings.Builder
	quote := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			quote = !quote
		case unicode.IsSpace(r) && !quote:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

func levenshteinAtMostOne(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	if len(ar)-len(br) > 1 || len(br)-len(ar) > 1 {
		return false
	}
	if len(ar) < len(br) {
		ar, br = br, ar
	}
	diff, j := 0, 0
	for i := 0; i < len(ar); i++ {
		if j < len(br) && ar[i] == br[j] {
			j++
			continue
		}
		diff++
		if diff > 1 {
			return false
		}
		if len(ar) == len(br) {
			j++
		}
	}
	return true
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
			b.WriteRune(' ')
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
