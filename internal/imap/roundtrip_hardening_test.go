package imap

// Hardening suite: full compose→wire→read-back verification with no network.
//
// Every message neomd sends is built by internal/smtp and later read by two
// consumers: the recipient's mail client (modeled here by go-message, the
// same RFC 5322/2045 parser neomd uses) and neomd itself (parseBody — inbox,
// Sent, draft-continue). These tests build real messages and assert
// BYTE-EXACT fidelity of every business-critical field: From, To, Cc,
// Subject, body text, attachment names and contents, threading headers, and
// the absence of Bcc / internal X-Neomd-* headers.
//
// Run with: go test ./internal/imap -run Hardening
// If a severe refactor breaks anything a client would receive, it fails here.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/sspaeti/neomd/internal/schedule"
	"github.com/sspaeti/neomd/internal/smtp"
)

// parsedMessage is what a standards-compliant recipient client sees.
type parsedMessage struct {
	rawHead  string // raw header block (for absence checks: Bcc, X-Neomd-*)
	subject  string // RFC 2047 decoded
	from     string // first From as "Name <addr>" (addr only when no name)
	to       []string
	cc       []string
	inReply  string
	refs     string
	msgID    string
	parts    []string          // inline content types, in order
	plain    string            // decoded text/plain body
	html     string            // decoded text/html body
	attached map[string][]byte // decoded attachment name → bytes
}

func fmtAddr(a *mail.Address) string {
	if a.Name != "" {
		return a.Name + " <" + a.Address + ">"
	}
	return a.Address
}

func parseBuilt(t *testing.T, raw []byte) parsedMessage {
	t.Helper()
	var pm parsedMessage
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		pm.rawHead = string(raw[:i])
	} else {
		t.Fatal("message has no header/body separator")
	}
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("go-message cannot parse built message: %v", err)
	}
	pm.subject, _ = r.Header.Subject()
	if froms, err := r.Header.AddressList("From"); err == nil && len(froms) > 0 {
		pm.from = fmtAddr(froms[0])
	}
	if tos, err := r.Header.AddressList("To"); err == nil {
		for _, a := range tos {
			pm.to = append(pm.to, fmtAddr(a))
		}
	}
	if ccs, err := r.Header.AddressList("Cc"); err == nil {
		for _, a := range ccs {
			pm.cc = append(pm.cc, fmtAddr(a))
		}
	}
	pm.inReply = r.Header.Get("In-Reply-To")
	pm.refs = r.Header.Get("References")
	pm.msgID = r.Header.Get("Message-ID")
	pm.attached = map[string][]byte{}
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			pm.parts = append(pm.parts, ct)
			body, _ := io.ReadAll(p.Body)
			switch ct {
			case "text/plain":
				pm.plain = string(body)
			case "text/html":
				pm.html = string(body)
			}
		case *mail.AttachmentHeader:
			name, _ := h.Filename()
			body, _ := io.ReadAll(p.Body)
			pm.attached[name] = body
		}
	}
	return pm
}

// hasHeaderLine reports whether any raw header line starts with prefix
// (case-insensitive) — used to prove headers are ABSENT on the wire.
func hasHeaderLine(rawHead, prefix string) bool {
	for _, line := range strings.Split(rawHead, "\r\n") {
		if len(line) >= len(prefix) && strings.EqualFold(line[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// binaryFixture writes a file containing every byte value (0-255 repeated) —
// any encoding corruption in the attachment pipeline changes at least one byte.
func binaryFixture(t *testing.T, name string) (path string, content []byte) {
	t.Helper()
	content = make([]byte, 4096)
	for i := range content {
		content[i] = byte(i % 256)
	}
	path = filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, content
}

// ── The golden round trip ────────────────────────────────────────────────

func TestHardening_RoundTrip_HeadersAndBody(t *testing.T) {
	from := "Simon Späti <simu@sspaeti.com>"
	to := "Louise Nachname <lnachname@domain.io>, second@client.example"
	cc := "cc-person@client.example"
	subject := "Ängebot für Züri — Q3 (rev. 2) 🚀"
	body := "Sehr geehrte Frau Nachname,\n\nanbei das **Angebot** für Zürich.\n\nFreundliche Grüsse\nSimon"

	raw, err := smtp.BuildMessage(from, to, cc, subject, body, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	pm := parseBuilt(t, raw)

	if pm.from != from {
		t.Errorf("From = %q, want %q", pm.from, from)
	}
	wantTo := []string{"Louise Nachname <lnachname@domain.io>", "second@client.example"}
	if len(pm.to) != 2 || pm.to[0] != wantTo[0] || pm.to[1] != wantTo[1] {
		t.Errorf("To = %v, want %v", pm.to, wantTo)
	}
	if len(pm.cc) != 1 || pm.cc[0] != cc {
		t.Errorf("Cc = %v, want [%s]", pm.cc, cc)
	}
	if pm.subject != subject {
		t.Errorf("Subject decoded = %q, want %q", pm.subject, subject)
	}
	// Body text must arrive byte-exact (umlauts survive quoted-printable).
	for _, line := range strings.Split(body, "\n") {
		if line != "" && !strings.Contains(pm.plain, line) {
			t.Errorf("plain part lost line %q", line)
		}
	}
	if !strings.Contains(pm.html, "Zürich") || !strings.Contains(pm.html, "<strong>Angebot</strong>") {
		t.Error("HTML part lost umlauts or markdown rendering")
	}
	// Structural invariants.
	if len(pm.parts) < 2 || pm.parts[0] != "text/plain" || pm.parts[1] != "text/html" {
		t.Errorf("part order = %v, want [text/plain text/html]", pm.parts)
	}
	if !strings.Contains(pm.msgID, "@sspaeti.com>") {
		t.Errorf("Message-ID %q must use sender domain", pm.msgID)
	}
	for _, forbidden := range []string{"Bcc:", "X-Neomd"} {
		if hasHeaderLine(pm.rawHead, forbidden) {
			t.Errorf("outgoing message must not carry %q header", forbidden)
		}
	}
	if pm.inReply != "" || pm.refs != "" {
		t.Error("non-reply must not carry threading headers")
	}
}

func TestHardening_RoundTrip_AttachmentFidelity(t *testing.T) {
	names := []string{
		"All issues - Ssp.pdf", // spaces + dashes (the draft-rename bug)
		"Rechnung März.pdf",    // non-ASCII
	}
	var paths []string
	want := map[string][]byte{}
	for _, n := range names {
		p, content := binaryFixture(t, n)
		paths = append(paths, p)
		want[n] = content
	}

	raw, err := smtp.BuildMessage("Simon <simu@sspaeti.com>", "louise@client.example", "",
		"attachment fidelity", "see attached", paths, "")
	if err != nil {
		t.Fatal(err)
	}

	// As the recipient's client sees it.
	pm := parseBuilt(t, raw)
	for _, n := range names {
		got, ok := pm.attached[n]
		if !ok {
			t.Errorf("recipient view: attachment %q missing (got %v)", n, keysOf(pm.attached))
			continue
		}
		if !bytes.Equal(got, want[n]) {
			t.Errorf("recipient view: attachment %q content corrupted (%d vs %d bytes)", n, len(got), len(want[n]))
		}
	}

	// As neomd itself re-reads it (inbox view, Sent copy, forward source).
	_, _, _, atts, _, _ := parseBody(raw)
	if len(atts) != len(names) {
		t.Fatalf("parseBody found %d attachments, want %d", len(atts), len(names))
	}
	for _, a := range atts {
		if !bytes.Equal(a.Data, want[a.Filename]) {
			t.Errorf("parseBody: attachment %q content mismatch", a.Filename)
		}
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestHardening_RoundTrip_ThreadingHeaders(t *testing.T) {
	// Contract: callers pass the ORIGINAL message's References chain; the
	// builder appends In-Reply-To itself (and never duplicates it, even for
	// broken senders whose References already contain their own Message-ID).
	for _, origRefs := range []string{
		"<root@client.example>",
		"<root@client.example> <orig-id@client.example>", // broken sender
	} {
		raw, err := smtp.BuildMessageWithThreading("Simon <s@ssp.sh>", "a@b.io", "",
			"Re: deal", "reply body", nil, "", "<orig-id@client.example>", origRefs)
		if err != nil {
			t.Fatal(err)
		}
		pm := parseBuilt(t, raw)
		if pm.inReply != "<orig-id@client.example>" {
			t.Errorf("In-Reply-To = %q", pm.inReply)
		}
		if pm.refs != "<root@client.example> <orig-id@client.example>" {
			t.Errorf("refs(%q): References = %q, want single orig-id at end", origRefs, pm.refs)
		}
	}
}

// Drafts must keep Bcc and survive re-reading with body and attachments intact
// (a corrupted draft round-trip means the re-sent email differs from what the
// user reviewed).
func TestHardening_RoundTrip_DraftWithAttachment(t *testing.T) {
	body := "Draft body line 1\n\n**bold** stays literal\n"
	path, content := binaryFixture(t, "All issues - Ssp.pdf")

	raw, err := smtp.BuildDraftMessage("Simon <s@ssp.sh>", "louise@client.example",
		"cc@x.io", "hidden@x.io", "Draft Ängebot", body, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if !hasHeaderLine(string(raw[:bytes.Index(raw, []byte("\r\n\r\n"))]), "Bcc:") {
		t.Error("draft must keep Bcc (it is re-opened, not delivered)")
	}
	md, _, _, atts, _, _ := parseBody(raw)
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line != "" && !strings.Contains(md, line) {
			t.Errorf("draft body lost line %q on re-open, got:\n%s", line, md)
		}
	}
	if len(atts) != 1 || atts[0].Filename != "All issues - Ssp.pdf" {
		t.Fatalf("draft attachment name mangled: %+v", attNames(atts))
	}
	if !bytes.Equal(atts[0].Data, content) {
		t.Error("draft attachment content corrupted")
	}
}

func attNames(atts []Attachment) []string {
	out := make([]string, len(atts))
	for i, a := range atts {
		out[i] = a.Filename
	}
	return out
}

// The send-later queue must deliver EXACTLY the bytes an immediate send would
// have produced — and never leak the X-Neomd-Rcpt header (it contains Bcc).
func TestHardening_RoundTrip_SendLaterDeliversIdenticalBytes(t *testing.T) {
	raw, err := smtp.BuildMessage("Simon <s@ssp.sh>", "louise@client.example", "",
		"scheduled offer", "body", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	queued := schedule.Inject(raw, time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC),
		[]string{"louise@client.example", "hidden-bcc@x.io"})
	job, cleaned, found, err := schedule.Extract(queued)
	if err != nil || !found {
		t.Fatalf("Extract: found=%v err=%v", found, err)
	}
	if !bytes.Equal(cleaned, raw) {
		t.Error("delivered bytes differ from immediate-send bytes")
	}
	if len(job.Rcpt) != 2 {
		t.Errorf("RCPT list = %v", job.Rcpt)
	}
	pm := parseBuilt(t, cleaned)
	if hasHeaderLine(pm.rawHead, "X-Neomd") {
		t.Error("delivered message leaks X-Neomd header (contains Bcc!)")
	}
}

// ── Header injection ─────────────────────────────────────────────────────
//
// Attacker-influenced strings (recipient fields parsed from the editor
// prelude, harvested contact names, forwarded attachment filenames) must
// never be able to smuggle extra headers into an outgoing message.

func TestHardening_HeaderInjection(t *testing.T) {
	const evil = "X-Injected"

	t.Run("subject CRLF", func(t *testing.T) {
		raw, err := smtp.BuildMessage("Simon <s@ssp.sh>", "a@b.io", "",
			"innocent\r\n"+evil+": 1", "body", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if pm := parseBuilt(t, raw); hasHeaderLine(pm.rawHead, evil) {
			t.Fatal("CRLF in subject injected a header")
		}
	})

	t.Run("to and cc CRLF", func(t *testing.T) {
		raw, err := smtp.BuildMessage("Simon <s@ssp.sh>",
			"victim@x.io\r\n"+evil+"-To: evil@x.io",
			"cc@x.io\r\n"+evil+"-Cc: evil@x.io",
			"s", "body", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if pm := parseBuilt(t, raw); hasHeaderLine(pm.rawHead, evil) {
			t.Fatal("CRLF in recipient field injected a header")
		}
	})

	t.Run("threading IDs CRLF", func(t *testing.T) {
		raw, err := smtp.BuildMessageWithThreading("Simon <s@ssp.sh>", "a@b.io", "",
			"s", "body", nil, "",
			"<id@x>\r\n"+evil+": 1", "<r@x>\r\n"+evil+": 2")
		if err != nil {
			t.Fatal(err)
		}
		if pm := parseBuilt(t, raw); hasHeaderLine(pm.rawHead, evil) {
			t.Fatal("CRLF in threading headers injected a header")
		}
	})

	t.Run("attachment filename quote and newline", func(t *testing.T) {
		// Linux allows quotes and newlines in filenames; a forwarded
		// attachment arrives under its sender-chosen name.
		dir := t.TempDir()
		path := filepath.Join(dir, "evil\"name\n"+evil+": 1.txt")
		if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
			t.Skip("filesystem rejects hostile filename")
		}
		raw, err := smtp.BuildMessage("Simon <s@ssp.sh>", "a@b.io", "", "s", "body", []string{path}, "")
		if err != nil {
			t.Fatal(err)
		}
		pm := parseBuilt(t, raw) // must stay parseable
		if hasHeaderLine(pm.rawHead, evil) {
			t.Fatal("hostile filename injected a top-level header")
		}
		for _, line := range strings.Split(string(raw), "\r\n") {
			if strings.HasPrefix(line, evil) {
				t.Fatalf("hostile filename injected a MIME part header: %q", line)
			}
		}
		if len(pm.attached) != 1 {
			t.Fatalf("attachment lost: %v", keysOf(pm.attached))
		}
	})
}
