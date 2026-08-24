package ui

// Hardening: the send path's recipient handling. What lands in SMTP RCPT TO
// decides WHO receives the email — it must be complete (To+Cc+Bcc), deduped,
// bare addresses only, and identical whether or not contact-name decoration
// touched the headers. Run with: go test ./internal/ui -run Hardening

import (
	"reflect"
	"strings"
	"testing"
)

func TestHardening_RcptListCompleteAndBare(t *testing.T) {
	to := "Louise Nachname <lnachname@domain.io>, second@client.example"
	cc := "cc-person@client.example, lnachname@domain.io" // dup with To
	bcc := "hidden@archive.example"

	got := collectRcptTo(to, cc, bcc)
	want := []string{"lnachname@domain.io", "second@client.example", "cc-person@client.example", "hidden@archive.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RCPT = %v, want %v", got, want)
	}
	for _, a := range got {
		if strings.ContainsAny(a, "<> \"") || !strings.Contains(a, "@") {
			t.Errorf("RCPT entry %q is not a bare address", a)
		}
	}
}

// Contact-name decoration rewrites headers only — the RCPT list computed from
// the raw fields must stay identical, and decoration must never alter which
// address a part points at.
func TestHardening_DecorationNeverChangesRecipients(t *testing.T) {
	s := testStore(t) // knows Louise Nachname <lnachname@domain.io>
	to := "lnachname@domain.io, second@client.example"
	cc := "cc-person@client.example"
	bcc := "hidden@archive.example"

	rawRcpt := collectRcptTo(to, cc, bcc)
	decoratedRcpt := collectRcptTo(s.Decorate(to), s.Decorate(cc), bcc)
	if !reflect.DeepEqual(rawRcpt, decoratedRcpt) {
		t.Fatalf("decoration changed the recipient set: %v vs %v", rawRcpt, decoratedRcpt)
	}

	// Bcc must never be decorated (it must not gain header-worthy content).
	if got := s.Decorate("lnachname@domain.io"); !strings.HasSuffix(got, "<lnachname@domain.io>") {
		t.Errorf("decoration lost the address itself: %q", got)
	}
}
