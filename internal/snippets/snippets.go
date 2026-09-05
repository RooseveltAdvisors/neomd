// Package snippets loads reusable email templates from a directory of
// markdown files. One file = one snippet; the filename (without extension) is
// the snippet name shown in the picker.
//
// An optional `Subject: ...` line at the top of the file sets the subject; the
// remainder (after the first blank line) is the body. Files without that line
// are pure bodies.
package snippets

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Snippet is one loaded template.
type Snippet struct {
	Name    string
	Subject string
	Body    string
}

// Load reads every *.md / *.txt file in dir, sorted by name. A missing or
// unreadable directory yields no snippets and no error — the picker just shows
// its empty-state hint. Unreadable individual files are skipped.
func Load(dir string) []Snippet {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Snippet
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(ent.Name()))
		if ext != ".md" && ext != ".txt" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			continue
		}
		s := Parse(strings.TrimSuffix(ent.Name(), filepath.Ext(ent.Name())), string(raw))
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Parse splits raw template text into subject and body. Leading `Subject:`
// lines are consumed as the subject; everything after the header block (and
// the blank line that terminates it) is the body.
func Parse(name, raw string) Snippet {
	s := Snippet{Name: name}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	i := 0
	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			// Blank line ends the header block only once a header was seen;
			// otherwise it is body whitespace we skip past anyway.
			if s.Subject != "" {
				i++
			}
			break
		}
		if rest, ok := cutHeader(line, "subject:"); ok && s.Subject == "" {
			s.Subject = rest
			continue
		}
		break
	}
	s.Body = strings.TrimLeft(strings.Join(lines[i:], "\n"), "\n")
	return s
}

// cutHeader matches a case-insensitive `name: value` prefix.
func cutHeader(line, name string) (string, bool) {
	if len(line) < len(name) || !strings.EqualFold(line[:len(name)], name) {
		return "", false
	}
	return strings.TrimSpace(line[len(name):]), true
}
