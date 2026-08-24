package contacts

import (
	"os"
	"path/filepath"
	"testing"
)

// Manual smoke test against a real contacts export (not run in CI):
//
//	NEOMD_CONTACTS_FILE=~/path/to/contacts.csv go test ./internal/contacts -run RealImport -v
func TestRealImportManual(t *testing.T) {
	path := os.Getenv("NEOMD_CONTACTS_FILE")
	if path == "" {
		t.Skip("set NEOMD_CONTACTS_FILE to run")
	}
	cache := filepath.Join(t.TempDir(), "cache")
	s := Load(cache)
	if err := s.MergeFile(path); err != nil {
		t.Fatalf("MergeFile: %v", err)
	}
	if err := s.SaveIfDirty(); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(cache)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	n := 0
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	if n == 0 {
		t.Fatal("no contacts imported")
	}
	t.Logf("imported %d address→name entries from %s", n, path)
}
