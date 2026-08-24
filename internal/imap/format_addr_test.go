package imap

import (
	"testing"

	imap "github.com/emersion/go-imap/v2"
)

// To/CC/BCC keep display names ("Name <addr>") so searching a person's name
// matches in the Sent folder; unsafe names fall back to the bare address so
// SplitAddrs / RCPT comma-splitting never breaks.
func TestFormatEnvelopeAddr(t *testing.T) {
	cases := []struct {
		name string
		addr imap.Address
		want string
	}{
		{"with name", imap.Address{Name: "Louise Nachname", Mailbox: "lnachname", Host: "domain.io"}, "Louise Nachname <lnachname@domain.io>"},
		{"no name", imap.Address{Mailbox: "lnachname", Host: "domain.io"}, "lnachname@domain.io"},
		{"name equals addr", imap.Address{Name: "l@domain.io", Mailbox: "l", Host: "domain.io"}, "l@domain.io"},
		{"comma in name", imap.Address{Name: "Nachname, Louise", Mailbox: "l", Host: "domain.io"}, "l@domain.io"},
		{"angle bracket in name", imap.Address{Name: "x <y>", Mailbox: "l", Host: "domain.io"}, "l@domain.io"},
		{"whitespace name", imap.Address{Name: "  ", Mailbox: "l", Host: "domain.io"}, "l@domain.io"},
	}
	for _, c := range cases {
		if got := formatEnvelopeAddr(c.addr); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
