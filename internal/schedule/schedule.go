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
	"regexp"
	"strconv"
	"strings"
	"time"
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

var (
	durationRe = regexp.MustCompile(`^\+?(?:(\d+)d)?(\d+[hm].*)?$`)
	clockRe    = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
)

// ParseSendAt turns a user-typed schedule expression into an absolute time.
// Supported forms (always interpreted in now's location):
//
//	+2h  +30m  +1d  +1d2h   relative offset from now
//	17:30                   next occurrence of that clock time (today or tomorrow)
//	tomorrow                tomorrow 09:00
//	tomorrow 17:30
//	2026-08-25 17:30        absolute date and time
func ParseSendAt(input string, now time.Time) (time.Time, error) {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "" {
		return time.Time{}, fmt.Errorf("empty schedule time")
	}

	var at time.Time
	switch {
	case s == "tomorrow":
		y, mo, d := now.AddDate(0, 0, 1).Date()
		at = time.Date(y, mo, d, 9, 0, 0, 0, now.Location())

	case strings.HasPrefix(s, "tomorrow "):
		m := clockRe.FindStringSubmatch(strings.TrimSpace(strings.TrimPrefix(s, "tomorrow ")))
		if m == nil {
			return time.Time{}, fmt.Errorf("expected: tomorrow HH:MM")
		}
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		if h > 23 || mi > 59 {
			return time.Time{}, fmt.Errorf("invalid clock time %q", s)
		}
		y, mo, d := now.AddDate(0, 0, 1).Date()
		at = time.Date(y, mo, d, h, mi, 0, 0, now.Location())

	case clockRe.MatchString(s):
		m := clockRe.FindStringSubmatch(s)
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		if h > 23 || mi > 59 {
			return time.Time{}, fmt.Errorf("invalid clock time %q", s)
		}
		y, mo, d := now.Date()
		at = time.Date(y, mo, d, h, mi, 0, 0, now.Location())
		if !at.After(now) {
			at = at.AddDate(0, 0, 1) // already passed today → tomorrow
		}

	case durationRe.MatchString(s) && strings.Trim(s, "+") != "":
		m := durationRe.FindStringSubmatch(s)
		var total time.Duration
		if m[1] != "" {
			days, _ := strconv.Atoi(m[1])
			total += time.Duration(days) * 24 * time.Hour
		}
		if m[2] != "" {
			d, err := time.ParseDuration(m[2])
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid duration %q", input)
			}
			total += d
		}
		if total <= 0 {
			return time.Time{}, fmt.Errorf("invalid duration %q", input)
		}
		at = now.Add(total)

	default:
		var err error
		at, err = time.ParseInLocation("2006-01-02 15:04", s, now.Location())
		if err != nil {
			return time.Time{}, fmt.Errorf("unrecognized time %q — use +2h, 17:30, tomorrow 09:00, or 2026-08-25 17:30", input)
		}
	}

	if !at.After(now) {
		return time.Time{}, fmt.Errorf("%s is in the past", at.Format("2006-01-02 15:04"))
	}
	return at, nil
}
