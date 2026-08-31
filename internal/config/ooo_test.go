package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ooo.toml next to config.toml overrides the whole [ooo] block, so the
// vacation settings can be synced to the headless server as a single file
// (make ooo) without touching the server's main config.

func TestLoadOOOOverride_FileReplacesBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ooo.toml")
	content := `
enabled = true
until   = "2026-09-07"
subject = "Away"
body    = "I'm out."
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadOOOOverride(path)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected override, got nil")
	}
	if !got.Enabled || got.Until != "2026-09-07" || got.Subject != "Away" || got.Body != "I'm out." {
		t.Fatalf("unexpected override: %+v", got)
	}
}

func TestLoadOOOOverride_MissingFileIsNil(t *testing.T) {
	got, err := LoadOOOOverride(filepath.Join(t.TempDir(), "ooo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("missing file must yield nil override, got %+v", got)
	}
}

func TestLoadOOOOverride_BadTOMLIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ooo.toml")
	if err := os.WriteFile(path, []byte("enabled = maybe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOOOOverride(path); err == nil {
		t.Fatal("invalid TOML must return an error (daemon logs it, fails safe)")
	}
}
