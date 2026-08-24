package schedule

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInjectExtractRoundTrip(t *testing.T) {
	raw := []byte("From: Simon <simu@sspaeti.com>\r\n" +
		"To: Louise Nachname <l@domain.io>\r\n" +
		"Subject: hello\r\n" +
		"\r\n" +
		"body line 1\r\n\r\nbody line 2\r\n")
	at := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	rcpt := []string{"l@domain.io", "hidden-bcc@x.io"}

	queued := Inject(raw, at, rcpt)
	job, cleaned, found, err := Extract(queued)
	if err != nil || !found {
		t.Fatalf("Extract: found=%v err=%v", found, err)
	}
	if !job.SendAt.Equal(at) {
		t.Errorf("SendAt = %v, want %v", job.SendAt, at)
	}
	if !reflect.DeepEqual(job.Rcpt, rcpt) {
		t.Errorf("Rcpt = %v, want %v", job.Rcpt, rcpt)
	}
	if job.From != "Simon <simu@sspaeti.com>" {
		t.Errorf("From = %q", job.From)
	}
	// Delivered message must not leak scheduling headers (Rcpt contains Bcc!)
	if strings.Contains(string(cleaned), "X-Neomd") {
		t.Errorf("cleaned message still contains X-Neomd headers:\n%s", cleaned)
	}
	if string(cleaned) != string(raw) {
		t.Errorf("cleaned != original:\n%q\nwant\n%q", cleaned, raw)
	}
}

// Regular mail the user moved to Scheduled (GTD) has no send-at header and
// must be left untouched.
func TestExtractIgnoresRegularMail(t *testing.T) {
	raw := []byte("From: a@b.io\r\nSubject: gtd item\r\n\r\nbody\r\n")
	_, cleaned, found, err := Extract(raw)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if found {
		t.Error("found = true for message without send-at header")
	}
	if string(cleaned) != string(raw) {
		t.Error("regular message was modified")
	}
}

func TestParseSendAt(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Zurich")
	now := time.Date(2026, 8, 24, 14, 30, 0, 0, loc)

	cases := []struct {
		in   string
		want time.Time
	}{
		{"+2h", now.Add(2 * time.Hour)},
		{"30m", now.Add(30 * time.Minute)},
		{"+1d", now.Add(24 * time.Hour)},
		{"+1d2h", now.Add(26 * time.Hour)},
		{"17:30", time.Date(2026, 8, 24, 17, 30, 0, 0, loc)},
		{"09:00", time.Date(2026, 8, 25, 9, 0, 0, 0, loc)}, // passed today → tomorrow
		{"tomorrow", time.Date(2026, 8, 25, 9, 0, 0, 0, loc)},
		{"Tomorrow 17:30", time.Date(2026, 8, 25, 17, 30, 0, 0, loc)},
		{"2026-08-25 08:15", time.Date(2026, 8, 25, 8, 15, 0, 0, loc)},
	}
	for _, c := range cases {
		got, err := ParseSendAt(c.in, now)
		if err != nil {
			t.Errorf("ParseSendAt(%q): %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("ParseSendAt(%q) = %v, want %v", c.in, got, c.want)
		}
	}

	for _, bad := range []string{"", "yesterday", "25:99", "2020-01-01 00:00", "+0m", "banana"} {
		if _, err := ParseSendAt(bad, now); err == nil {
			t.Errorf("ParseSendAt(%q): expected error", bad)
		}
	}
}
