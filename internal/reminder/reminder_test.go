package reminder

import (
	"bytes"
	"testing"
	"time"
)

func TestParseHeaderAndStatus(t *testing.T) {
	at := time.Date(2030, time.January, 2, 8, 4, 5, 0, time.UTC)
	got, err := ParseHeader([]byte("X-Neomd-Reminder-At: 2030-01-02T03:04:05-05:00\r\nX-Neomd-Reminder-ID: id-1\r\n\r\n"))
	if err != nil || !got.At.Equal(at) || got.State != "scheduled" || got.ID != "id-1" {
		t.Fatalf("metadata=%#v err=%v", got, err)
	}
	if got.Status(at.Add(-time.Minute)) != "scheduled" || got.Status(at) != "due" {
		t.Fatal("reminder status did not follow its due time")
	}
}

func TestHeadersPreserveBodyAndReplaceFoldedValues(t *testing.T) {
	raw := []byte("Subject: test\r\nX-Neomd-Reminder-At: old\r\n folded\r\nX-Unrelated: yes\r\n\r\nbody\r\n")
	updated, err := SetHeaders(raw, time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), "id")
	if err != nil || !bytes.HasSuffix(updated, []byte("body\r\n")) || bytes.Contains(updated, []byte("folded")) {
		t.Fatalf("updated=%q err=%v", updated, err)
	}
	cleared, err := ClearHeaders(updated)
	if err != nil || bytes.Contains(cleared, []byte("Reminder")) || !bytes.HasSuffix(cleared, []byte("body\r\n")) {
		t.Fatalf("cleared=%q err=%v", cleared, err)
	}
}

func TestHeadersPreserveBodyWithMixedLineEndings(t *testing.T) {
	raw := []byte("Subject: x\n\nbody-one\nbody-two\r\n\r\nbody-tail")
	wantBody := []byte("body-one\nbody-two\r\n\r\nbody-tail")
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	updated, err := SetHeaders(raw, at, "id")
	if err != nil || !bytes.HasSuffix(updated, wantBody) {
		t.Fatalf("updated body=%q err=%v", updated, err)
	}
	cleared, err := ClearHeaders(updated)
	if err != nil || !bytes.HasSuffix(cleared, wantBody) {
		t.Fatalf("cleared body=%q err=%v", cleared, err)
	}
}
