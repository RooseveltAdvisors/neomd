// Package contacts maintains a small persistent address book harvested from
// message headers: lower-cased email address → display name.
//
// It exists because messages neomd sends carry bare addresses (the user types
// "l@domain.io", not "Louise Nachname <l@domain.io>"), so searching a person's
// name in the Sent folder finds nothing. Harvested names close that gap:
// searches expand a name to the known addresses, and outgoing To/Cc headers
// are decorated with the known display name so future messages carry it.
package contacts

import (
	"encoding/csv"
	"mime"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Store is a concurrency-safe address→name cache backed by a flat file
// ("addr\tname" per line).
type Store struct {
	mu    sync.Mutex
	path  string
	names map[string]string // lower-cased addr → display name
	dirty bool
}

// Load reads the cache file at path. A missing or unreadable file yields an
// empty store — harvesting will fill it.
func Load(path string) *Store {
	s := &Store{path: path, names: make(map[string]string)}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	for _, line := range strings.Split(string(data), "\n") {
		addr, name, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		addr = strings.ToLower(strings.TrimSpace(addr))
		name = strings.TrimSpace(name)
		if addr != "" && name != "" {
			s.names[addr] = name
		}
	}
	return s
}

// Add records a display name for an address. Empty, address-equal, or unsafe
// names (containing `,`, `<`, `>`, `"` — they would break the comma-separated
// header fields the name is later injected into) are ignored.
func (s *Store) Add(addr, name string) {
	if s == nil {
		return
	}
	addr = strings.ToLower(strings.TrimSpace(addr))
	name = strings.TrimSpace(name)
	if addr == "" || !strings.Contains(addr, "@") ||
		name == "" || strings.EqualFold(name, addr) || strings.ContainsAny(name, `,<>"`) {
		return
	}
	// Names end up in outgoing To/Cc headers (Decorate) — control characters
	// (CR/LF above all) would allow header injection from a hostile sender's
	// display name. Reject the whole name rather than trying to repair it.
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.names[addr] != name {
		s.names[addr] = name
		s.dirty = true
	}
}

// HarvestField records every "Name <addr>" pair found in a comma-separated
// address header field. Bare addresses carry no name and are skipped.
func (s *Store) HarvestField(field string) {
	if s == nil || field == "" {
		return
	}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		i := strings.IndexByte(part, '<')
		j := strings.IndexByte(part, '>')
		if i <= 0 || j <= i {
			continue
		}
		s.Add(part[i+1:j], part[:i])
	}
}

// Name returns the known display name for an address, or "".
func (s *Store) Name(addr string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.names[strings.ToLower(strings.TrimSpace(addr))]
}

// Entry is one address book row.
type Entry struct {
	Addr string
	Name string
}

// All returns every known contact, sorted by name then address (for the
// contacts picker).
func (s *Store) All() []Entry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	out := make([]Entry, 0, len(s.names))
	for addr, name := range s.names {
		out = append(out, Entry{Addr: addr, Name: name})
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].Addr < out[j].Addr
	})
	return out
}

// AddrsMatchingName returns up to limit addresses whose display name contains
// query (case-insensitive), sorted for determinism.
func (s *Store) AddrsMatchingName(query string, limit int) []string {
	if s == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	s.mu.Lock()
	var out []string
	for addr, name := range s.names {
		if strings.Contains(strings.ToLower(name), q) {
			out = append(out, addr)
		}
	}
	s.mu.Unlock()
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Decorate rewrites bare addresses in a comma-separated recipient field to
// "Name <addr>" for addresses with a known display name. Parts that already
// carry a name (contain '<') are left untouched. Intended for message headers
// only — RCPT TO extraction must keep using the undecorated field.
func (s *Store) Decorate(field string) string {
	if s == nil || strings.TrimSpace(field) == "" {
		return field
	}
	parts := strings.Split(field, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "<") {
			name := s.Name(p)
			if name == "" {
				name = DeriveName(p) // first.last@domain → "First Last"
			}
			if name != "" {
				// Q-encodes non-ASCII names (RFC 2047); ASCII passes through.
				p = mime.QEncoding.Encode("utf-8", name) + " <" + p + ">"
			}
		}
		out = append(out, p)
	}
	return strings.Join(out, ", ")
}

// derivableLocalRe matches local parts shaped like a person's name:
// two or more purely alphabetic segments separated by "." / "_" / "-".
var derivableLocalRe = regexp.MustCompile(`^[a-zA-Z]+(?:[._-][a-zA-Z]+)+$`)

// roleWords are local-part segments that indicate a functional mailbox, not a
// person — never derive a display name from those.
var roleWords = map[string]bool{
	"admin": true, "billing": true, "contact": true, "help": true,
	"hello": true, "info": true, "mail": true, "newsletter": true,
	"no": true, "noreply": true, "office": true, "reply": true,
	"sales": true, "service": true, "support": true, "team": true,
}

// DeriveName guesses a display name from a "first.last@domain" shaped address
// ("example.name@domain.io" → "Example Name"). Returns "" when the local part
// doesn't look like a person's name (single segment, digits, role mailboxes).
// Used only as a fallback when no harvested or user-provided name exists.
func DeriveName(addr string) string {
	local, _, ok := strings.Cut(strings.TrimSpace(addr), "@")
	if !ok || !derivableLocalRe.MatchString(local) {
		return ""
	}
	segs := strings.FieldsFunc(local, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	for i, seg := range segs {
		if roleWords[strings.ToLower(seg)] {
			return ""
		}
		segs[i] = strings.ToUpper(seg[:1]) + strings.ToLower(seg[1:])
	}
	return strings.Join(segs, " ")
}

// MergeFile merges a user-maintained contacts file into the store. Two
// formats are auto-detected: a Google Contacts CSV export (header row with
// "E-mail 1 - Value" columns; contacts.google.com → Export → Google CSV), and
// simple lines — "addr,name", "addr<TAB>name", or "Name <addr>", with #
// comments. Missing file is fine (returns nil): the feature is optional.
func (s *Store) MergeFile(path string) error {
	if s == nil || path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text := strings.TrimPrefix(string(data), "\ufeff") // Google exports may carry a UTF-8 BOM
	if strings.Contains(strings.SplitN(text, "\n", 2)[0], "E-mail 1 - Value") {
		return s.mergeGoogleCSV(text)
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '<'); i > 0 { // "Name <addr>" form
			s.HarvestField(line)
			continue
		}
		addr, name, ok := strings.Cut(line, "\t")
		if !ok {
			addr, name, ok = strings.Cut(line, ",")
		}
		if ok {
			s.Add(addr, name)
		}
	}
	return nil
}

// mergeGoogleCSV imports name + email columns from a Google Contacts export.
func (s *Store) mergeGoogleCSV(text string) error {
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return err
	}
	if len(rows) < 2 {
		return nil
	}
	nameCol, firstCol, middleCol, lastCol := -1, -1, -1, -1
	var mailCols []int
	for i, h := range rows[0] {
		switch {
		case h == "Name":
			nameCol = i
		case h == "First Name":
			firstCol = i
		case h == "Middle Name":
			middleCol = i
		case h == "Last Name":
			lastCol = i
		case strings.HasPrefix(h, "E-mail ") && strings.HasSuffix(h, "- Value"):
			mailCols = append(mailCols, i)
		}
	}
	cell := func(row []string, i int) string {
		if i >= 0 && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	for _, row := range rows[1:] {
		name := cell(row, nameCol)
		if name == "" {
			name = strings.Join(strings.Fields(
				cell(row, firstCol)+" "+cell(row, middleCol)+" "+cell(row, lastCol)), " ")
		}
		if name == "" {
			continue
		}
		for _, mc := range mailCols {
			// Google separates multiple addresses in one cell with " ::: ".
			for _, addr := range strings.Split(cell(row, mc), ":::") {
				s.Add(addr, name)
			}
		}
	}
	return nil
}

// SaveIfDirty atomically writes the store to disk when it changed since load
// or the last save. Safe to call from a background goroutine.
func (s *Store) SaveIfDirty() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.dirty || s.path == "" {
		s.mu.Unlock()
		return nil
	}
	addrs := make([]string, 0, len(s.names))
	for addr := range s.names {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	var b strings.Builder
	for _, addr := range addrs {
		b.WriteString(addr)
		b.WriteString("\t")
		b.WriteString(s.names[addr])
		b.WriteString("\n")
	}
	s.dirty = false
	path := s.path
	s.mu.Unlock()

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
