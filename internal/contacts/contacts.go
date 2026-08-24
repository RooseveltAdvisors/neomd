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
	"mime"
	"os"
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
			if name := s.Name(p); name != "" {
				// Q-encodes non-ASCII names (RFC 2047); ASCII passes through.
				p = mime.QEncoding.Encode("utf-8", name) + " <" + p + ">"
			}
		}
		out = append(out, p)
	}
	return strings.Join(out, ", ")
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
