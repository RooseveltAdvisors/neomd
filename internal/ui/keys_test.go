package ui

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDocumentedKeyBindings keeps the four user-facing surfaces aligned:
// docs/keys.md, the ? overlay, and the key names implemented by the TUI. It
// also catches accidental duplicate rows inside one help context.
func TestDocumentedKeyBindings(t *testing.T) {
	doc, err := os.Open(filepath.Join("..", "..", "docs", "keys.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	handlerKeys := keyLiteralsInHandlers(t)

	helpText := make([]string, 0)
	seen := make(map[string]bool)
	for _, section := range HelpSections {
		for _, row := range section.Rows {
			if seen[section.Title+"\x00"+row[0]] {
				t.Fatalf("duplicate %q in help section %q", row[0], section.Title)
			}
			seen[section.Title+"\x00"+row[0]] = true
			helpText = append(helpText, row[0])
		}
	}

	scanner := bufio.NewScanner(doc)
	rows := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|---") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 5 || strings.TrimSpace(cells[1]) == "Superhuman key" {
			continue
		}
		neomdKey := strings.ReplaceAll(strings.TrimSpace(cells[2]), "`", "")
		status := strings.TrimSpace(cells[3])
		if status == "dropped" || neomdKey == "dropped" {
			continue
		}
		rows++
		if !documentedKeyAppearsInHelp(neomdKey, helpText) {
			t.Errorf("docs/keys.md binding %q is absent from HelpSections", neomdKey)
		}
		if !documentedKeyIsHandled(neomdKey, handlerKeys) {
			t.Errorf("docs/keys.md binding %q is absent from inbox/reader key handlers", neomdKey)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 40 {
		t.Fatalf("only checked %d documented bindings; expected the complete map", rows)
	}
}

func keyLiteralsInHandlers(t *testing.T) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "model.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		switch fn.Name.Name {
		case "Update", "updateInbox", "updateReader", "updatePresend", "handleChord":
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(lit.Value)
				if err == nil {
					keys[value] = true
				}
				return true
			})
		}
	}
	return keys
}

func documentedKeyIsHandled(docKey string, handlers map[string]bool) bool {
	if strings.Contains(docKey, "subject field") {
		return true
	}
	if docKey == "/" || docKey == "?" {
		return handlers[docKey]
	}
	if strings.Contains(docKey, "<space>") {
		return handlers[" "]
	}
	if strings.Contains(docKey, "gg") && handlers["g"] {
		return true
	}
	if strings.HasPrefix(docKey, "g") && handlers["g"] {
		return true
	}
	if strings.HasPrefix(docKey, ":") {
		return handlers[":"]
	}
	for _, part := range strings.Split(docKey, "/") {
		part = strings.TrimSpace(strings.Trim(part, "`"))
		if part == "" {
			continue
		}
		if fields := strings.Fields(part); len(fields) > 0 {
			part = fields[0]
		}
		if handlers[part] {
			return true
		}
		if part == "dd" && handlers["d"] {
			return true
		}
	}
	return false
}

func documentedKeyAppearsInHelp(docKey string, help []string) bool {
	if strings.Contains(docKey, "subject field") {
		return true // compose's subject is a text field, not a one-key action.
	}
	if docKey == "/" {
		return true
	}
	if strings.Contains(docKey, "<space>1") {
		return true
	}
	for _, candidate := range help {
		for _, token := range strings.FieldsFunc(docKey, func(r rune) bool {
			return r == '/' || r == ' ' || r == '`'
		}) {
			token = strings.TrimSpace(token)
			if token != "" && strings.Contains(candidate, token) {
				return true
			}
		}
	}
	return false
}
