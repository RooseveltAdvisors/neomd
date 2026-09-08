// Package schedule implements neomd's send-later queue on top of IMAP.
//
// Scheduling appends the fully built MIME message to the Scheduled folder with
// two extra headers: X-Neomd-Send-At (RFC 3339 delivery time) and X-Neomd-Rcpt
// (the complete RCPT TO list including Bcc — the queue lives in the user's own
// mailbox and both headers are stripped before delivery). The headless daemon
// (`neomd --headless`) scans the folder each sync cycle; when a message is due
// it claims it with \Flagged, strips the X-Neomd-* headers, delivers via SMTP,
// copies the clean message to Sent, and deletes the queue entry. Deleting a
// message from the Scheduled folder cancels the send.
package schedule

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/sspaeti/neomd/internal/when"
)

// Header names injected into queued messages.
const (
	HeaderSendAt = "X-Neomd-Send-At"
	HeaderRcpt   = "X-Neomd-Rcpt"
)

// Job describes one due send-later message parsed from a queued raw MIME.
type Job struct {
	SendAt time.Time
	Rcpt   []string // full RCPT TO list (To + Cc + Bcc)
	From   string   // From header — resolves the SMTP account
}

// Inject prepends the scheduling headers to a raw MIME message.
func Inject(raw []byte, sendAt time.Time, rcpt []string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s: %s\r\n", HeaderSendAt, sendAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "%s: %s\r\n", HeaderRcpt, strings.Join(rcpt, ", "))
	b.Write(raw)
	return b.Bytes()
}

// Extract parses the scheduling headers out of a queued message and returns
// the job plus the message with the X-Neomd-* headers removed (what actually
// gets delivered and saved to Sent). found is false when the message carries
// no X-Neomd-Send-At header — e.g. regular mail the user moved to the
// Scheduled folder for GTD purposes — such messages must be left alone.
func Extract(raw []byte) (job Job, cleaned []byte, found bool, err error) {
	// Split header block from body at the first blank line; tolerate both
	// CRLF (our own builder) and bare LF line endings.
	sep, sepLen := []byte("\r\n\r\n"), 4
	idx := bytes.Index(raw, sep)
	if idx < 0 {
		sep, sepLen = []byte("\n\n"), 2
		idx = bytes.Index(raw, sep)
	}
	if idx < 0 {
		return Job{}, raw, false, nil // no header/body separator — not ours
	}
	head, body := raw[:idx], raw[idx+sepLen:]

	var kept []string
	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimRight(line, "\r")
		key, val, ok := strings.Cut(line, ":")
		val = strings.TrimSpace(val)
		switch {
		case ok && strings.EqualFold(key, HeaderSendAt):
			found = true
			job.SendAt, err = time.Parse(time.RFC3339, val)
			if err != nil {
				return Job{}, raw, true, fmt.Errorf("parse %s %q: %w", HeaderSendAt, val, err)
			}
		case ok && strings.EqualFold(key, HeaderRcpt):
			for _, a := range strings.Split(val, ",") {
				if a = strings.TrimSpace(a); a != "" {
					job.Rcpt = append(job.Rcpt, a)
				}
			}
		default:
			if ok && strings.EqualFold(key, "From") {
				job.From = val
			}
			kept = append(kept, line)
		}
	}
	if !found {
		return Job{}, raw, false, nil
	}
	if len(job.Rcpt) == 0 {
		return Job{}, raw, true, fmt.Errorf("queued message has %s but no %s recipients", HeaderSendAt, HeaderRcpt)
	}
	var b bytes.Buffer
	for _, line := range kept {
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.Write(body)
	return job, b.Bytes(), true, nil
}

// RewriteDate replaces the message's Date header with t (RFC 5322 format).
// The Date is stamped at BUILD time, but a queued message is delivered
// minutes to days later — without this, the recipient's client (and neomd's
// own Sent view) shows the moment the user pressed `l`, not when the email
// was actually sent. Only the Date header line changes; every other byte is
// preserved. A message without a Date header is returned unchanged.
func RewriteDate(raw []byte, t time.Time) []byte {
	sep, sepLen := []byte("\r\n\r\n"), 4
	idx := bytes.Index(raw, sep)
	if idx < 0 {
		sep, sepLen = []byte("\n\n"), 2
		idx = bytes.Index(raw, sep)
	}
	if idx < 0 {
		return raw
	}
	head, body := raw[:idx], raw[idx+sepLen:]
	lines := strings.Split(string(head), "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if key, _, ok := strings.Cut(trimmed, ":"); ok && strings.EqualFold(key, "Date") {
			lines[i] = "Date: " + t.Format(time.RFC1123Z)
			replaced = true
			break
		}
	}
	if !replaced {
		return raw
	}
	var b bytes.Buffer
	for _, line := range lines {
		b.WriteString(strings.TrimRight(line, "\r"))
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.Write(body)
	return b.Bytes()
}

// ParseSendAt turns a user-typed schedule expression into an absolute time.
// Deprecated: use when.Parse. Kept as a compatibility wrapper for existing
// callers of the scheduling package.
func ParseSendAt(input string, now time.Time) (time.Time, error) {
	return when.Parse(input, now)
}
