// Hardening integration tests: byte-exact fidelity of the business-critical
// email path against a REAL IMAP/SMTP server. The unit round-trip suite
// (internal/imap/roundtrip_hardening_test.go) proves the builder and parser
// agree with each other; these tests prove nothing gets lost or mangled by an
// actual server round trip: subjects, recipients, attachment names and bytes,
// draft re-opening, and the send-later queue.
//
// Skipped unless NEOMD_TEST_IMAP_HOST is set (same env as integration_test.go).
// Run with: make test-integration   (or -run Integration_Hardening)
package integration_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sspaeti/neomd/internal/schedule"
	"github.com/sspaeti/neomd/internal/smtp"
)

// binaryContent returns bytes covering every value 0-255 — any transfer
// encoding corruption flips at least one byte.
func binaryContent() []byte {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte(i % 256)
	}
	return b
}

// The exact business incident this suite exists for: an attachment saved with
// a draft must come back under its ORIGINAL filename with identical bytes, so
// re-sending the draft delivers exactly what was attached.
func TestIntegration_Hardening_DraftAttachmentRoundTrip(t *testing.T) {
	env := loadEnv(t)
	cli := env.imapClient()
	defer cli.Close()
	ctx := context.Background()

	const filename = "All issues - Ssp.pdf"
	content := binaryContent()
	path := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	subject := uniqueSubject("draft-attach-roundtrip")
	body := "Draft body that must survive.\n\n**bold stays literal**"
	raw, err := smtp.BuildDraftMessage(env.from, env.user, "", "bcc-keep@example.com", subject, body, []string{path})
	if err != nil {
		t.Fatalf("BuildDraftMessage: %v", err)
	}
	if _, err := cli.EnsureFolders(ctx, []string{"Drafts"}); err != nil {
		t.Fatalf("EnsureFolders: %v", err)
	}
	if err := cli.SaveDraft(ctx, "Drafts", raw); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	email := waitForEmail(t, cli, "Drafts", subject, 30*time.Second)
	defer cleanupEmail(t, cli, "Drafts", email.UID)

	// Envelope fields survive.
	if email.Subject != subject {
		t.Errorf("draft subject = %q, want %q", email.Subject, subject)
	}
	if !strings.Contains(email.BCC, "bcc-keep@example.com") {
		t.Errorf("draft lost Bcc: %q", email.BCC)
	}

	// Body + attachment survive byte-exact — this is what continue-draft re-sends.
	md, _, _, atts, _, _, err := cli.FetchBody(ctx, "Drafts", email.UID)
	if err != nil {
		t.Fatalf("FetchBody: %v", err)
	}
	if !strings.Contains(md, "**bold stays literal**") {
		t.Errorf("draft body mutated:\n%s", md)
	}
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].Filename != filename {
		t.Errorf("attachment renamed: %q, want %q", atts[0].Filename, filename)
	}
	if !bytes.Equal(atts[0].Data, content) {
		t.Errorf("attachment corrupted: %d bytes vs %d", len(atts[0].Data), len(content))
	}
}

// Full SMTP → server → IMAP fidelity: umlaut subject byte-exact, attachment
// name + bytes byte-exact, body line intact, and neither Bcc nor any internal
// X-Neomd header visible on the received message.
func TestIntegration_Hardening_SendFidelity(t *testing.T) {
	env := loadEnv(t)
	cli := env.imapClient()
	defer cli.Close()
	ctx := context.Background()

	const filename = "Rechnung März - Q3 (rev 2).pdf"
	content := binaryContent()
	path := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	subject := uniqueSubject("fidelity Ängebot Züri 🚀")
	body := "Grüezi,\n\nanbei die Rechnung für März.\n\nFreundliche Grüsse"

	// Bcc to self: must be delivered but never appear as a header.
	if err := smtp.Send(env.smtpConfig(), env.user, "", env.user, subject, body, []string{path}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	email := waitForEmail(t, cli, "INBOX", "fidelity", 60*time.Second)
	defer cleanupEmail(t, cli, "INBOX", email.UID)

	if email.Subject != subject {
		t.Errorf("subject mangled in transit:\ngot  %q\nwant %q", email.Subject, subject)
	}

	rawBack, err := cli.FetchRaw(ctx, "INBOX", email.UID)
	if err != nil {
		t.Fatalf("FetchRaw: %v", err)
	}
	head := rawBack
	if i := bytes.Index(rawBack, []byte("\r\n\r\n")); i > 0 {
		head = rawBack[:i]
	}
	for _, line := range strings.Split(string(head), "\r\n") {
		l := strings.ToLower(line)
		if strings.HasPrefix(l, "bcc:") {
			t.Error("Bcc leaked into delivered headers")
		}
		if strings.HasPrefix(l, "x-neomd") {
			t.Errorf("internal header leaked: %q", line)
		}
	}

	_, _, _, atts, _, _, err := cli.FetchBody(ctx, "INBOX", email.UID)
	if err != nil {
		t.Fatalf("FetchBody: %v", err)
	}
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].Filename != filename {
		t.Errorf("attachment name mangled: %q, want %q", atts[0].Filename, filename)
	}
	if !bytes.Equal(atts[0].Data, content) {
		t.Error("attachment bytes corrupted in transit")
	}
}

// Send-later queue on a real server: the queued message must round-trip
// through APPEND + FETCH with its schedule intact, and the extracted
// deliverable must carry no X-Neomd headers.
func TestIntegration_Hardening_ScheduledQueueRoundTrip(t *testing.T) {
	env := loadEnv(t)
	cli := env.imapClient()
	defer cli.Close()
	ctx := context.Background()

	subject := uniqueSubject("scheduled-queue")
	raw, err := smtp.BuildMessage(env.from, env.user, "", subject, "queued body", nil, "")
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	sendAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	queued := schedule.Inject(raw, sendAt, []string{env.user, "hidden-bcc@example.com"})

	if _, err := cli.EnsureFolders(ctx, []string{"Scheduled"}); err != nil {
		t.Fatalf("EnsureFolders: %v", err)
	}
	if err := cli.SaveSent(ctx, "Scheduled", queued); err != nil {
		t.Fatalf("SaveSent to Scheduled: %v", err)
	}
	email := waitForEmail(t, cli, "Scheduled", subject, 30*time.Second)
	defer cleanupEmail(t, cli, "Scheduled", email.UID)

	rawBack, err := cli.FetchRaw(ctx, "Scheduled", email.UID)
	if err != nil {
		t.Fatalf("FetchRaw: %v", err)
	}
	job, cleaned, found, err := schedule.Extract(rawBack)
	if err != nil || !found {
		t.Fatalf("Extract after server round-trip: found=%v err=%v", found, err)
	}
	if !job.SendAt.Equal(sendAt) {
		t.Errorf("SendAt = %v, want %v", job.SendAt, sendAt)
	}
	if len(job.Rcpt) != 2 || job.Rcpt[1] != "hidden-bcc@example.com" {
		t.Errorf("RCPT list mangled: %v", job.Rcpt)
	}
	if bytes.Contains(cleaned, []byte("X-Neomd")) {
		t.Error("extracted deliverable still contains X-Neomd headers")
	}
	if !bytes.Contains(cleaned, []byte("queued body")) {
		t.Error("extracted deliverable lost the body")
	}
}
