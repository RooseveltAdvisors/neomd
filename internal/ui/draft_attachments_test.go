package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sspaeti/neomd/internal/imap"
)

// Pinning test for the draft attachment rename bug: continuing a draft must
// keep each attachment's original filename (the sent filename is derived from
// the temp path's basename), never a mangled CreateTemp name like
// "draft-Offer.pdf-718599635".
func TestWriteAttachmentsTempPreservesFilename(t *testing.T) {
	files := []imap.Attachment{
		{Filename: "All issues - Ssp.pdf", Data: []byte("%PDF-1.4 first")},
		{Filename: "All issues - Ssp.pdf", Data: []byte("%PDF-1.4 second")},
		{Filename: "../../evil.sh", Data: []byte("nope")},
		{Filename: "", Data: []byte("unnamed")},
	}
	paths, err := writeAttachmentsTemp(files)
	if err != nil {
		t.Fatalf("writeAttachmentsTemp: %v", err)
	}
	defer func() {
		if len(paths) > 0 {
			os.RemoveAll(filepath.Dir(paths[0]))
		}
	}()
	if len(paths) != len(files) {
		t.Fatalf("got %d paths, want %d", len(paths), len(files))
	}

	wantNames := []string{
		"All issues - Ssp.pdf",
		"All issues - Ssp-2.pdf", // duplicate deduped, extension kept
		"evil.sh",    // path traversal stripped
		"attachment", // empty name fallback
	}
	for i, p := range paths {
		if got := filepath.Base(p); got != wantNames[i] {
			t.Errorf("attachment %d: basename = %q, want %q", i, got, wantNames[i])
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("attachment %d unreadable: %v", i, err)
		}
		if string(data) != string(files[i].Data) {
			t.Errorf("attachment %d: content mismatch", i)
		}
	}
}
