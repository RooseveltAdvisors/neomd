// Package attachments contains the shared safety rules for local and received
// email attachments.
package attachments

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

// MaxBytes is the largest attachment or MIME part neomd will materialize in
// memory. A bounded part keeps a hostile message or an accidentally selected
// giant local file from turning the TUI into an unbounded allocator.
const MaxBytes = 25 << 20

var ErrTooLarge = errors.New("attachment exceeds 25 MiB limit")

// ValidateFile accepts only an existing, non-symlink regular file within the
// supported size limit. Lstat is intentional: following a picker-provided
// symlink would make the file selected by the user differ from the file
// neomd later reads.
func ValidateFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("attachment path is empty")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat attachment: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink attachments are not allowed")
	}
	if !info.Mode().IsRegular() {
		return errors.New("attachment is not a regular file")
	}
	if info.Size() > MaxBytes {
		return ErrTooLarge
	}
	return nil
}

// ReadFile validates and reads a local attachment with a hard size cap. The
// second bounded read protects against a file growing after validation.
func ReadFile(path string) ([]byte, error) {
	if err := ValidateFile(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open attachment: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	closeErr := f.Close()
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close attachment: %w", closeErr)
	}
	if len(data) > MaxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

// ReadAll reads one decoded MIME part without allowing unbounded allocation.
func ReadAll(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

// SafeFilename returns a single filesystem-safe basename for a sender-
// supplied filename. It handles both slash conventions, removes control
// characters, and prevents . / .. from escaping or aliasing the destination.
func SafeFilename(name, fallback string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `/\\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	if name == "" || name == "." || name == ".." {
		name = fallback
	}
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	// Stay below common filesystem component limits while preserving valid
	// UTF-8. The attachment's extension is retained where possible.
	if len([]byte(name)) > 240 {
		ext := ""
		if dot := strings.LastIndexByte(name, '.'); dot > 0 {
			ext = name[dot:]
		}
		maxStem := 240 - len([]byte(ext))
		stem := []rune(strings.TrimSuffix(name, ext))
		for len([]byte(string(stem))) > maxStem && len(stem) > 0 {
			stem = stem[:len(stem)-1]
		}
		name = string(stem) + ext
	}
	return name
}
