// Package search contains NeoMD's private, memory-only full-text index.
//
// IMAP remains the source of truth. The index stores only decoded searchable
// text and message identity metadata for the current process; it is rebuilt
// from authorized folders and never persisted to disk.
package search

import (
	"context"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/web"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/sspaeti/neomd/internal/imap"
)

const (
	fieldFrom    = "from"
	fieldTo      = "to"
	fieldSubject = "subject"
	fieldBody    = "body"
)

type document struct {
	email       imap.Email
	contactText string
	body        string
	bodyReady   bool
	fingerprint string
	scope       string
}

// Index is safe to refresh and query from separate Bubble Tea commands. The
// inverted index is provided by Bleve; docs only retains identity and refresh
// metadata needed to reconcile IMAP scopes and route selected messages.
type Index struct {
	mu    sync.RWMutex
	docs  map[string]document
	bleve bleve.Index
}

// New returns an empty memory-only Bleve full-text index. The mapping is
// intentionally explicit: attachment bytes and arbitrary IMAP fields are not
// indexed or retained.
func New() *Index {
	field := mapping.NewTextFieldMapping()
	field.Analyzer = "web"
	field.Store = false
	field.IncludeTermVectors = false

	docMapping := mapping.NewDocumentMapping()
	docMapping.Dynamic = false
	for _, name := range []string{fieldFrom, fieldTo, fieldSubject, fieldBody} {
		docMapping.AddFieldMappingsAt(name, field)
	}
	indexMapping := mapping.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping
	indexMapping.DefaultAnalyzer = "web"

	idx, err := bleve.NewMemOnly(indexMapping)
	if err != nil {
		// The mapping uses only Bleve's built-in analyzer. Failure here means the
		// binary cannot provide its required local search primitive.
		panic("search: initialize Bleve index: " + err.Error())
	}
	return &Index{docs: make(map[string]document), bleve: idx}
}

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
	d, ok := i.docs[k]
	if ok && d.bodyReady && d.fingerprint == fingerprint(e) && d.contactText == contactText {
		d.email, d.scope = e, scope(e)
		i.docs[k] = d
		return
	}
	if ok && d.bodyReady && d.fingerprint == fingerprint(e) {
		d.email, d.contactText, d.scope = e, contactText, scope(e)
		i.docs[k] = d
		i.putLocked(k, d)
		return
	}
	d = document{email: e, contactText: contactText, fingerprint: fingerprint(e), scope: scope(e)}
	i.docs[k] = d
	i.putLocked(k, d)
}

// UpsertBody stores decoded plain/HTML text for a message.
func (i *Index) UpsertBody(e imap.Email, contactText, plain, html string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	k := Key(e)
	d := document{
		email:       e,
		contactText: contactText,
		body:        plain + "\n" + stripHTML(html),
		bodyReady:   true,
		fingerprint: fingerprint(e),
		scope:       scope(e),
	}
	i.docs[k] = d
	i.putLocked(k, d)
}

func (i *Index) putLocked(k string, d document) {
	if err := i.bleve.Index(k, bleveDocument(d)); err != nil {
		log.Printf("search index update failed: %v", err)
	}
}

func bleveDocument(d document) map[string]string {
	return map[string]string{
		fieldFrom:    d.email.From + " " + d.contactText,
		fieldTo:      d.email.To + " " + d.email.CC + " " + d.email.BCC + " " + d.contactText,
		fieldSubject: d.email.Subject,
		fieldBody:    d.body,
	}
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
				if err := i.bleve.Delete(k); err != nil {
					log.Printf("search index delete failed: %v", err)
				}
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

type parsedQuery struct {
	from, to, subject []string
	free              []string
}

func parseQuery(text string) parsedQuery {
	var q parsedQuery
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

// Search returns matching emails in relevance/date order.
func (i *Index) Search(text string) []imap.Email { return i.SearchContext(context.Background(), text) }

// SearchContext is Search with cancellation propagated into Bleve's searcher.
func (i *Index) SearchContext(ctx context.Context, text string) []imap.Email {
	q := parseQuery(text)
	i.mu.RLock()
	defer i.mu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	searchQuery := buildQuery(q)
	size := len(i.docs)
	if size == 0 {
		return nil
	}
	result, err := i.bleve.SearchInContext(ctx, bleve.NewSearchRequestOptions(searchQuery, size, 0, false))
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("search query failed: %v", err)
		}
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	type scored struct {
		e     imap.Email
		score float64
	}
	hits := make([]scored, 0, len(result.Hits))
	for _, hit := range result.Hits {
		if d, ok := i.docs[hit.ID]; ok {
			hits = append(hits, scored{e: d.email, score: hit.Score})
		}
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

func buildQuery(q parsedQuery) query.Query {
	var must []query.Query
	if len(q.from) > 0 {
		must = append(must, termsQuery(q.from, []string{fieldFrom}))
	}
	if len(q.to) > 0 {
		must = append(must, termsQuery(q.to, []string{fieldTo}))
	}
	if len(q.subject) > 0 {
		must = append(must, termsQuery(q.subject, []string{fieldSubject}))
	}
	if len(q.free) > 0 {
		must = append(must, termsQuery(q.free, []string{fieldFrom, fieldTo, fieldSubject, fieldBody}))
	}
	if len(must) == 0 {
		return bleve.NewMatchAllQuery()
	}
	return bleve.NewConjunctionQuery(must...)
}

func termsQuery(terms, fields []string) query.Query {
	var must []query.Query
	for _, term := range terms {
		var should []query.Query
		for _, field := range fields {
			exact := bleve.NewTermQuery(term)
			exact.SetField(field)
			exact.SetBoost(3)
			prefix := bleve.NewPrefixQuery(term)
			prefix.SetField(field)
			prefix.SetBoost(2)
			fieldTerm := bleve.NewDisjunctionQuery(exact, prefix)
			if len([]rune(term)) >= 4 {
				fuzzy := bleve.NewFuzzyQuery(term)
				fuzzy.SetField(field)
				fuzzy.SetBoost(1)
				fieldTerm.AddQuery(fuzzy)
			}
			should = append(should, fieldTerm)
		}
		must = append(must, bleve.NewDisjunctionQuery(should...))
	}
	return bleve.NewConjunctionQuery(must...)
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
