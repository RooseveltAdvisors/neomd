package attachments

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFileRejectsUnsafeAndOversizedPaths(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(regular, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	tooLarge := filepath.Join(dir, "large.bin")
	if err := os.WriteFile(tooLarge, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(tooLarge, MaxBytes+1); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pdf")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}

	if err := ValidateFile(regular); err != nil {
		t.Fatalf("regular file rejected: %v", err)
	}
	for name, path := range map[string]string{
		"directory": dir,
		"missing":   filepath.Join(dir, "missing.txt"),
		"symlink":   link,
		"too large": tooLarge,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateFile(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestReadAllIsBounded(t *testing.T) {
	_, err := ReadAll(strings.NewReader(strings.Repeat("x", MaxBytes+1)))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("ReadAll error = %v, want ErrTooLarge", err)
	}
	if _, err := ReadAll(io.LimitReader(strings.NewReader("ok"), MaxBytes)); err != nil {
		t.Fatalf("small ReadAll failed: %v", err)
	}
}

func TestSafeFilenameRemovesPathAndControlCharacters(t *testing.T) {
	tests := map[string]string{
		"../../evil.sh":                   "evil.sh",
		`C:\\Users\\evil.exe`:             "evil.exe",
		"bad\nname\t.pdf":                 "bad_name_.pdf",
		"..":                              "fallback.bin",
		"":                                "fallback.bin",
		strings.Repeat("a", 300) + ".pdf": strings.Repeat("a", 236) + ".pdf",
	}
	for input, want := range tests {
		if got := SafeFilename(input, "fallback.bin"); got != want {
			t.Errorf("SafeFilename(%q) = %q, want %q", input, got, want)
		}
	}
}
