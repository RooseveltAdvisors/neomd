// Package reminder defines the metadata used to park an email until later.
package reminder

import (
	"bytes"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

const (
	AtHeader    = "X-Neomd-Reminder-At"
	StateHeader = "X-Neomd-Reminder-State"
	IDHeader    = "X-Neomd-Reminder-ID"
)

type Metadata struct {
	At    time.Time
	State string
	ID    string
}

// ParseHeader parses only the reminder headers supplied by IMAP BODY.PEEK.
func ParseHeader(raw []byte) (Metadata, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Metadata{}, fmt.Errorf("parse reminder headers: %w", err)
	}
	atValue := strings.TrimSpace(msg.Header.Get(AtHeader))
	if atValue == "" {
		return Metadata{}, nil
	}
	at, err := time.Parse(time.RFC3339, atValue)
	if err != nil {
		return Metadata{}, fmt.Errorf("parse %s: %w", AtHeader, err)
	}
	state := strings.ToLower(strings.TrimSpace(msg.Header.Get(StateHeader)))
	if state == "" {
		state = "scheduled"
	}
	if state != "scheduled" && state != "due" {
		return Metadata{}, fmt.Errorf("invalid reminder state %q", state)
	}
	return Metadata{At: at.UTC(), State: state, ID: strings.TrimSpace(msg.Header.Get(IDHeader))}, nil
}

func (m Metadata) Status(now time.Time) string {
	if m.State != "scheduled" || m.At.IsZero() {
		return m.State
	}
	if !now.Before(m.At) {
		return "due"
	}
	return "scheduled"
}

// SetHeaders adds or replaces reminder metadata and leaves the message body
// byte-for-byte untouched. It accepts CRLF or LF input and emits CRLF headers.
func SetHeaders(raw []byte, at time.Time, id string) ([]byte, error) {
	if at.IsZero() {
		return nil, fmt.Errorf("reminder time is required")
	}
	if strings.ContainsAny(id, "\r\n") {
		return nil, fmt.Errorf("reminder ID contains a newline")
	}
	return rewrite(raw, map[string]string{
		AtHeader:    at.UTC().Format(time.RFC3339),
		StateHeader: "scheduled",
		IDHeader:    id,
	})
}

// ClearHeaders removes reminder metadata without changing the body.
func ClearHeaders(raw []byte) ([]byte, error) {
	return rewrite(raw, nil)
}

func rewrite(raw []byte, values map[string]string) ([]byte, error) {
	crlfIdx := bytes.Index(raw, []byte("\r\n\r\n"))
	lfIdx := bytes.Index(raw, []byte("\n\n"))
	idx, sepLen := crlfIdx, 4
	if idx < 0 || (lfIdx >= 0 && lfIdx < idx) {
		idx, sepLen = lfIdx, 2
	}
	if idx < 0 {
		return nil, fmt.Errorf("message has no RFC header/body separator")
	}
	var lines []string
	removing := false
	for _, line := range strings.Split(string(raw[:idx]), "\n") {
		line = strings.TrimRight(line, "\r")
		if removing && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			continue
		}
		removing = false
		key, _, ok := strings.Cut(line, ":")
		if ok && (strings.EqualFold(key, AtHeader) || strings.EqualFold(key, StateHeader) || strings.EqualFold(key, IDHeader)) {
			removing = true
			continue
		}
		lines = append(lines, line)
	}
	if len(values) > 0 {
		// Keep metadata together at the top, making it cheap to fetch later.
		injected := []string{AtHeader + ": " + values[AtHeader], StateHeader + ": " + values[StateHeader], IDHeader + ": " + values[IDHeader]}
		lines = append(injected, lines...)
	}
	var out bytes.Buffer
	for _, line := range lines {
		out.WriteString(line)
		out.WriteString("\r\n")
	}
	out.WriteString("\r\n")
	out.Write(raw[idx+sepLen:])
	return out.Bytes(), nil
}
