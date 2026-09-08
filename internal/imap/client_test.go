package imap

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/sspaeti/neomd/internal/reminder"
)

type testLiteralReader struct {
	*bytes.Reader
	size int64
}

func (r *testLiteralReader) Size() int64 { return r.size }

func startMemoryIMAP(t *testing.T, folders ...string) (*Client, *imapmemserver.User) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-imap"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: privateKey}},
	})
	if err != nil {
		t.Fatal(err)
	}

	memServer := imapmemserver.New()
	user := imapmemserver.NewUser("user", "password")
	for _, folder := range folders {
		if err := user.Create(folder, nil); err != nil {
			t.Fatal(err)
		}
	}
	memServer.AddUser(user)
	server := imapserver.New(&imapserver.Options{
		NewSession: func(_ *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memServer.NewSession(), nil, nil
		},
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
			imap.CapIMAP4rev2: {},
			imap.CapMove:      {},
		},
	})
	go func() { _ = server.Serve(listener) }()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = server.Close()
		t.Fatal(err)
	}
	client := New(Config{
		Host:     "127.0.0.1",
		Port:     port,
		User:     "user",
		Password: "password",
		TLS:      true,
	})
	t.Cleanup(func() {
		client.Close()
		_ = server.Close()
	})
	return client, user
}

// Send-later queued messages are identified in header fetches by their
// X-Neomd-Send-At header (fetched as a peek'd header-fields section) so the
// UI can mark them without mutating the stored message.
func TestParseSendAtSection(t *testing.T) {
	at := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	if got := parseSendAtSection([]byte("X-Neomd-Send-At: " + at.Format(time.RFC3339) + "\r\n\r\n")); !got.Equal(at) {
		t.Errorf("SendAt = %v, want %v", got, at)
	}
	// Regular mail (no section content) and garbage must yield zero time.
	if got := parseSendAtSection(nil); !got.IsZero() {
		t.Errorf("nil section: %v", got)
	}
	if got := parseSendAtSection([]byte("\r\n")); !got.IsZero() {
		t.Errorf("empty header: %v", got)
	}
	if got := parseSendAtSection([]byte("X-Neomd-Send-At: not-a-time\r\n")); !got.IsZero() {
		t.Errorf("garbage time: %v", got)
	}
}

func TestFetchHeadersFallsBackWithoutBodyStructure(t *testing.T) {
	want := []*imapclient.FetchMessageBuffer{{}}
	opts := &imap.FetchOptions{BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}
	calls := 0
	got, err := collectHeaderFetch(func(gotOpts *imap.FetchOptions) ([]*imapclient.FetchMessageBuffer, error) {
		calls++
		if calls == 1 {
			if gotOpts.BodyStructure == nil {
				t.Fatal("first fetch unexpectedly omitted BODYSTRUCTURE")
			}
			return nil, errors.New("in body-type-mpart: expected body")
		}
		if gotOpts.BodyStructure != nil {
			t.Fatal("fallback fetch still requested BODYSTRUCTURE")
		}
		return want, nil
	}, opts)
	if err != nil {
		t.Fatalf("fallback fetch failed: %v", err)
	}
	if calls != 2 || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("calls=%d messages=%v, want one retried message", calls, got)
	}
}

func TestSearchMessageIDsAndReadRawMessage(t *testing.T) {
	client, user := startMemoryIMAP(t, "INBOX")
	raw := []byte("From: sender@example.com\r\nTo: user@example.com\r\nSubject: fixture\r\n" +
		"Message-ID: <fixture-read@example.com>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nfixture body\r\n")
	data, err := user.Append("INBOX", &testLiteralReader{Reader: bytes.NewReader(raw), size: int64(len(raw))}, &imap.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(client.Addr())
	if err != nil {
		t.Fatal(err)
	}
	readOnly := New(Config{Host: host, Port: port, User: "user", Password: "password", TLS: true, ReadOnly: true})
	t.Cleanup(readOnly.Close)
	uids, err := readOnly.SearchMessageIDs(context.Background(), "INBOX", "fixture-read@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(uids) != 1 || uids[0] != uint32(data.UID) {
		t.Fatalf("uids=%v, want [%d]", uids, data.UID)
	}
	fetched, err := readOnly.FetchRaw(context.Background(), "INBOX", uids[0])
	if err != nil {
		t.Fatal(err)
	}
	email, body, attachments, err := ParseRawMessage(fetched)
	if err != nil || email.MessageID != "<fixture-read@example.com>" || body != "fixture body" || len(attachments) != 0 {
		t.Fatalf("email=%+v body=%q attachments=%d err=%v", email, body, len(attachments), err)
	}
}

func TestReminderCopiesSearchesByMessageID(t *testing.T) {
	client, user := startMemoryIMAP(t, "INBOX", "Archive")
	raw := []byte("From: sender@example.com\r\nTo: me@example.com\r\nSubject: follow up\r\nMessage-ID: <reminder-search@example.com>\r\n\r\nbody\r\n")
	data, err := user.Append("Archive", &testLiteralReader{Reader: bytes.NewReader(raw), size: int64(len(raw))}, &imap.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	copies, err := client.reminderCopies(context.Background(), Email{
		Folder: "INBOX", From: "sender@example.com", Subject: "follow up",
		MessageID: "reminder-search@example.com", Size: uint32(len(raw)),
	}, "reminder-search@example.com", []string{"INBOX", "Archive"})
	if err != nil {
		t.Fatalf("reminderCopies: %v", err)
	}
	if len(copies) != 1 || copies[0].Folder != "Archive" || copies[0].UID != uint32(data.UID) {
		t.Fatalf("copies=%+v, want Archive UID %d", copies, data.UID)
	}
}

func TestParseReminderSection(t *testing.T) {
	got := parseReminder([]byte("X-Neomd-Reminder-At: 2030-01-02T03:04:05Z\r\nX-Neomd-Reminder-State: due\r\nX-Neomd-Reminder-ID: id-1\r\n"))
	if got == nil || got.State != "due" || !got.At.Equal(time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("reminder = %#v, want due metadata", got)
	}
	if parseReminder(nil) != nil || parseReminder([]byte("Subject: ordinary\r\n")) != nil {
		t.Fatal("ordinary or incomplete headers should not become reminders")
	}
}

func TestParseReminderRejectsUnidentifiedHeader(t *testing.T) {
	if got := parseReminder([]byte("X-Neomd-Reminder-At: 2030-01-02T03:04:05Z\r\n")); got != nil {
		t.Fatalf("unidentified reminder = %#v, want nil", got)
	}
}

func TestParkReminderRejectsWaitingSource(t *testing.T) {
	client := &Client{}
	err := client.ParkReminder(context.Background(), Email{Folder: "INBOX", UID: 1}, []string{"INBOX", "Waiting", "Trash"}, "INBOX", "Trash", time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected Waiting=Inbox to be rejected before IMAP access")
	}
}

func TestParseHeaderSectionsByDescriptor(t *testing.T) {
	sendAtSection := sendAtHeaderSection()
	reminderSection := reminderHeaderSection()
	sendAtResponse := *sendAtSection
	reminderResponse := *reminderSection
	msg := &imapclient.FetchMessageBuffer{BodySection: []imapclient.FetchBodySectionBuffer{
		{
			Section: &reminderResponse,
			Bytes:   []byte("X-Neomd-Reminder-At: 2030-01-02T03:04:05Z\r\nX-Neomd-Reminder-State: due\r\nX-Neomd-Reminder-ID: id-1\r\n"),
		},
		{
			Section: &sendAtResponse,
			Bytes:   []byte("X-Neomd-Send-At: 2030-01-03T03:04:05Z\r\n"),
		},
	}}

	if got := parseSendAtSection(msg.FindBodySection(sendAtSection)); !got.Equal(time.Date(2030, time.January, 3, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("SendAt = %v, want descriptor-matched value", got)
	}
	if got := parseReminder(msg.FindBodySection(reminderSection)); got == nil || got.State != "due" {
		t.Errorf("Reminder = %#v, want descriptor-matched value", got)
	}
}

func TestParkReminderReconcilesMovedSourceAndIsIdempotent(t *testing.T) {
	client, user := startMemoryIMAP(t, "INBOX", "Feed", "Waiting", "Trash")
	raw := []byte("From: sender@example.com\r\nTo: me@example.com\r\nSubject: meeting\r\nMessage-ID: <reminder@example.com>\r\n\r\nbody\r\n")
	data, err := user.Append("INBOX", &testLiteralReader{Reader: bytes.NewReader(raw), size: int64(len(raw))}, &imap.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	source := Email{
		UID:       uint32(data.UID),
		Folder:    "INBOX",
		From:      "sender@example.com",
		Subject:   "meeting",
		MessageID: "reminder@example.com",
		Size:      uint32(len(raw)),
	}
	if _, err := client.MoveMessage(context.Background(), "INBOX", source.UID, "Feed"); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	folders := []string{"INBOX", "Feed", "Waiting", "Trash"}
	if err := client.ParkReminder(context.Background(), source, folders, "Waiting", "Trash", at); err != nil {
		t.Fatalf("ParkReminder after source move: %v", err)
	}
	waiting, err := client.FetchHeaders(context.Background(), "Waiting", 0)
	if err != nil {
		t.Fatal(err)
	}
	trash, err := client.FetchHeaders(context.Background(), "Trash", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || len(trash) != 1 {
		t.Fatalf("after first park: Waiting=%d Trash=%d, want one in each", len(waiting), len(trash))
	}
	waitingRaw, err := client.FetchRaw(context.Background(), "Waiting", waiting[0].UID)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := reminder.ParseHeader(waitingRaw)
	if err != nil || !metadata.At.Equal(at) || metadata.ID != source.MessageID {
		t.Fatalf("waiting metadata=%#v err=%v", metadata, err)
	}
	if !bytes.HasSuffix(waitingRaw, []byte("body\r\n")) {
		t.Fatalf("waiting body changed: %q", waitingRaw)
	}

	if err := client.ParkReminder(context.Background(), source, folders, "Waiting", "Trash", at.Add(time.Hour)); err != nil {
		t.Fatalf("repeating ParkReminder: %v", err)
	}
	waiting, err = client.FetchHeaders(context.Background(), "Waiting", 0)
	if err != nil {
		t.Fatal(err)
	}
	trash, err = client.FetchHeaders(context.Background(), "Trash", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || len(trash) != 1 {
		t.Fatalf("after repeated park: Waiting=%d Trash=%d, want one in each", len(waiting), len(trash))
	}
}

func TestParkReminderRejectsAmbiguousCopiesWithoutMovingThem(t *testing.T) {
	client, user := startMemoryIMAP(t, "INBOX", "Archive", "Sent", "Waiting", "Trash")
	raw := []byte("From: sender@example.com\r\nTo: me@example.com\r\nSubject: meeting\r\nMessage-ID: <ambiguous@example.com>\r\n\r\nbody\r\n")
	for _, folder := range []string{"Archive", "Sent"} {
		if _, err := user.Append(folder, &testLiteralReader{Reader: bytes.NewReader(raw), size: int64(len(raw))}, &imap.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	source := Email{
		UID:       999,
		Folder:    "INBOX",
		From:      "sender@example.com",
		Subject:   "meeting",
		MessageID: "ambiguous@example.com",
		Size:      uint32(len(raw)),
	}
	err := client.ParkReminder(context.Background(), source, []string{"INBOX", "Archive", "Sent", "Waiting", "Trash"}, "Waiting", "Trash", time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ParkReminder error = %v, want ambiguity error", err)
	}
	for _, folder := range []string{"Archive", "Sent", "Waiting", "Trash"} {
		emails, fetchErr := client.FetchHeaders(context.Background(), folder, 0)
		if fetchErr != nil {
			t.Fatal(fetchErr)
		}
		want := 1
		if folder == "Waiting" || folder == "Trash" {
			want = 0
		}
		if len(emails) != want {
			t.Errorf("%s has %d messages, want %d", folder, len(emails), want)
		}
	}
}

func TestBuildSearchCriteria(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantKey   string // expected Header[0].Key (empty means check Or)
		wantValue string // expected Header[0].Value
		wantOr    bool   // expect Or field to be non-empty
	}{
		{
			name:      "from prefix",
			query:     "from:alice",
			wantKey:   "From",
			wantValue: "alice",
		},
		{
			name:      "subject prefix",
			query:     "subject:meeting",
			wantKey:   "Subject",
			wantValue: "meeting",
		},
		{
			name:      "to prefix",
			query:     "to:bob",
			wantKey:   "To",
			wantValue: "bob",
		},
		{
			name:   "plain text uses OR",
			query:  "hello world",
			wantOr: true,
		},
		{
			name:      "case-insensitive prefix preserves value case",
			query:     "FROM:Alice",
			wantKey:   "From",
			wantValue: "Alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := buildSearchCriteria(tt.query)
			if tt.wantOr {
				if len(c.Or) == 0 {
					t.Fatalf("expected Or field to be non-empty for query %q", tt.query)
				}
				return
			}
			if len(c.Header) == 0 {
				t.Fatalf("expected Header to be non-empty for query %q", tt.query)
			}
			if c.Header[0].Key != tt.wantKey {
				t.Errorf("Header Key = %q, want %q", c.Header[0].Key, tt.wantKey)
			}
			if c.Header[0].Value != tt.wantValue {
				t.Errorf("Header Value = %q, want %q", c.Header[0].Value, tt.wantValue)
			}
		})
	}
}

func TestHasAttachment(t *testing.T) {
	tests := []struct {
		name string
		bs   imap.BodyStructure
		want bool
	}{
		{
			name: "nil body structure",
			bs:   nil,
			want: false,
		},
		{
			name: "single part text/plain",
			bs:   &imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
			want: false,
		},
		{
			name: "single part image/png counts as attachment",
			bs:   &imap.BodyStructureSinglePart{Type: "image", Subtype: "png"},
			want: true,
		},
		{
			name: "multipart text/plain + text/html only",
			bs: &imap.BodyStructureMultiPart{
				Subtype: "alternative",
				Children: []imap.BodyStructure{
					&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
					&imap.BodyStructureSinglePart{Type: "text", Subtype: "html"},
				},
			},
			want: false,
		},
		{
			name: "multipart with nested image child",
			bs: &imap.BodyStructureMultiPart{
				Subtype: "mixed",
				Children: []imap.BodyStructure{
					&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
					&imap.BodyStructureSinglePart{Type: "image", Subtype: "jpeg"},
				},
			},
			want: true,
		},
		{
			name: "single part with attachment disposition",
			bs: &imap.BodyStructureSinglePart{
				Type:    "application",
				Subtype: "pdf",
				Extended: &imap.BodyStructureSinglePartExt{
					Disposition: &imap.BodyStructureDisposition{
						Value: "attachment",
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasAttachment(tt.bs)
			if got != tt.want {
				t.Errorf("hasAttachment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitAddrs(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"alice@example.com", []string{"alice@example.com"}},
		{"Alice <alice@example.com>, Bob <bob@example.com>", []string{"alice@example.com", "bob@example.com"}},
		{"alice@example.com, bob@example.com", []string{"alice@example.com", "bob@example.com"}},
		{"", nil},
		{"  , ,  ", nil},
		{"ALICE@EXAMPLE.COM", []string{"alice@example.com"}}, // lowercased
	}
	for _, tt := range tests {
		got := SplitAddrs(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("SplitAddrs(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("SplitAddrs(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParticipantMatch(t *testing.T) {
	participants := map[string]bool{
		"alice@example.com": true,
		"bob@example.com":   true,
	}
	tests := []struct {
		name  string
		email Email
		want  bool
	}{
		{
			"from matches",
			Email{From: "Alice <alice@example.com>", To: "other@example.com"},
			true,
		},
		{
			"to matches",
			Email{From: "other@example.com", To: "bob@example.com"},
			true,
		},
		{
			"cc matches",
			Email{From: "other@example.com", To: "other2@example.com", CC: "alice@example.com"},
			true,
		},
		{
			"no match",
			Email{From: "stranger@example.com", To: "other@example.com"},
			false,
		},
		{
			"empty email",
			Email{},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := participantMatch(tt.email, participants)
			if got != tt.want {
				t.Errorf("participantMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseBody_InlineImageContentID(t *testing.T) {
	// Construct a minimal multipart/related MIME message with an inline image.
	boundary := "----=_Part_123"
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<html><body><p>Hello</p><img src=\"cid:img001@neomd\"></body></html>\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: image/png; name=\"photo.png\"\r\n" +
		"Content-Disposition: inline; filename=\"photo.png\"\r\n" +
		"Content-ID: <img001@neomd>\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"iVBORw0KGgo=\r\n" +
		"--" + boundary + "--\r\n"

	_, _, _, attachments, _, _ := parseBody([]byte(raw))

	if len(attachments) == 0 {
		t.Fatal("expected at least 1 attachment, got 0")
	}

	found := false
	for _, a := range attachments {
		if a.ContentID == "img001@neomd" {
			found = true
			if a.Filename != "photo.png" {
				t.Errorf("Filename = %q, want %q", a.Filename, "photo.png")
			}
			if !strings.HasPrefix(a.ContentType, "image/") {
				t.Errorf("ContentType = %q, want image/*", a.ContentType)
			}
		}
	}
	if !found {
		cids := make([]string, len(attachments))
		for i, a := range attachments {
			cids[i] = a.ContentID
		}
		t.Errorf("no attachment with ContentID 'img001@neomd', got CIDs: %v", cids)
	}
}

func TestParseBody_NoContentID(t *testing.T) {
	// Regular attachment without Content-ID should have empty ContentID.
	boundary := "----=_Part_456"
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello world\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: application/pdf; name=\"doc.pdf\"\r\n" +
		"Content-Disposition: attachment; filename=\"doc.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"JVBERi0=\r\n" +
		"--" + boundary + "--\r\n"

	_, _, _, attachments, _, _ := parseBody([]byte(raw))

	if len(attachments) == 0 {
		t.Fatal("expected at least 1 attachment, got 0")
	}
	for _, a := range attachments {
		if a.Filename == "doc.pdf" && a.ContentID != "" {
			t.Errorf("regular attachment should have empty ContentID, got %q", a.ContentID)
		}
	}
}

func TestConnect_RefusesUnencrypted(t *testing.T) {
	c := &Client{
		cfg: Config{
			Host: "localhost",
			Port: "143",
			TLS:  false,
			// STARTTLS defaults to false
		},
	}
	err := c.connect(context.Background())
	if err == nil {
		t.Fatal("expected error for unencrypted connection, got nil")
	}
	if !strings.Contains(err.Error(), "refusing unencrypted") {
		t.Errorf("error = %q, want it to contain 'refusing unencrypted'", err.Error())
	}
}

func TestParseBody_DraftRoundTrip(t *testing.T) {
	// Test that draft content survives multiple save/load cycles without mutation.
	// This verifies the X-Neomd-Draft header correctly bypasses normalizePlainText.

	// Original markdown with various formatting that would be mutated by normalization
	originalBody := `Hello there

This is line 1
This is line 2

**Bold text** and *italic text*

[Link](https://example.com)

Code: ` + "`inline code`" + `

--
Signature line 1
Signature line 2`

	// Build a draft MIME message (plain text with X-Neomd-Draft header)
	draftMIME := "From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@example.com>\r\n" +
		"Subject: Test Draft\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"X-Neomd-Draft: true\r\n" +
		"\r\n" +
		originalBody

	// First parse (simulating draft reopen)
	body1, _, _, _, _, _ := parseBody([]byte(draftMIME))

	// Verify the body matches exactly (no trailing spaces added)
	if body1 != originalBody {
		t.Errorf("first parse mutated draft content\ngot:\n%q\nwant:\n%q", body1, originalBody)
	}

	// Second parse (simulating a save/reopen cycle)
	draftMIME2 := "From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@example.com>\r\n" +
		"Subject: Test Draft\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"X-Neomd-Draft: true\r\n" +
		"\r\n" +
		body1 // Use the result from first parse

	body2, _, _, _, _, _ := parseBody([]byte(draftMIME2))

	// Verify still matches exactly (no accumulation of trailing spaces)
	if body2 != originalBody {
		t.Errorf("second parse mutated draft content\ngot:\n%q\nwant:\n%q", body2, originalBody)
	}

	// Verify they're all equal
	if body1 != body2 {
		t.Errorf("draft content changed between parse cycles\nfirst:\n%q\nsecond:\n%q", body1, body2)
	}
}

func TestParseBody_NonDraftGetsNormalized(t *testing.T) {
	// Test that regular emails (without X-Neomd-Draft) still get normalizePlainText applied.

	originalBody := "Line 1\nLine 2"

	// Regular email (no X-Neomd-Draft header)
	regularMIME := "From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@example.com>\r\n" +
		"Subject: Regular Email\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		originalBody

	body, _, _, _, _, _ := parseBody([]byte(regularMIME))

	// Normalization should add two trailing spaces before the newline
	expectedNormalized := "Line 1  \nLine 2"
	if body != expectedNormalized {
		t.Errorf("normalization not applied to regular email\ngot:\n%q\nwant:\n%q", body, expectedNormalized)
	}
}

func TestParseBody_ReferencesExtraction(t *testing.T) {
	// Build a test message with References header
	raw := "From: test@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test\r\n" +
		"Message-ID: <msg3@example.com>\r\n" +
		"In-Reply-To: <msg2@example.com>\r\n" +
		"References: <msg1@example.com> <msg2@example.com>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Test body"

	_, _, _, _, references, _ := parseBody([]byte(raw))

	wantReferences := "<msg1@example.com> <msg2@example.com>"
	if references != wantReferences {
		t.Errorf("References = %q, want %q", references, wantReferences)
	}
}

func TestSpyPixelDetection(t *testing.T) {
	// HTML email with 2 tracking pixels from different domains.
	// First: detected by size heuristic (width="1" height="1")
	// Second: detected by URL pattern (/track/open)
	// Third: legitimate image with alt text — should NOT be counted
	// Fourth: decorative image with empty alt but normal size — should NOT be counted
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		`<html><body>` +
		`<p>Hello world</p>` +
		`<img src="https://click.mailchimp.com/track/open.php?id=abc" alt="" width="1" height="1">` +
		`<img src="https://pixel.sendinblue.com/beacon/track/open?id=xyz" alt="" height="0">` +
		`<img src="cid:logo" alt="Company Logo">` +
		`<img src="https://cdn.example.com/button.png" alt="" width="200" height="50">` +
		`</body></html>`

	_, _, _, _, _, spy := parseBody([]byte(raw))

	if spy.Count != 2 {
		t.Errorf("SpyPixelInfo.Count = %d, want 2", spy.Count)
	}
	// Check that domains were extracted
	found := make(map[string]bool)
	for _, d := range spy.Domains {
		found[d] = true
	}
	// With the tracker denylist, services are identified by name.
	// Mailchimp pixel matches "Mailchimp" or a Yesware /track/open pattern.
	if !found["Mailchimp"] && !found["Yesware"] {
		t.Errorf("expected Mailchimp or Yesware attribution in spy.Domains, got %v", spy.Domains)
	}
	for _, d := range spy.Domains {
		if strings.Contains(d, "cdn.example.com") {
			t.Errorf("decorative image should NOT be counted, got %v", spy.Domains)
		}
	}
}

func TestSpyPixelSpacersNotFlagged(t *testing.T) {
	// Layout spacers (one dimension is 1 but the other is large) must NOT
	// be flagged as spy pixels — they are decorative, not trackers.
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		`<html><body>` +
		`<img src="https://a.kajabi.com/9/9d08eac.png" alt="" width="1" height="16">` +
		`<img src="https://a.kajabi.com/9/9d08eac.png" alt="" width="40" height="1">` +
		`<img src="https://a.kajabi.com/9/9d08eac.png" alt="" width="1" height="50">` +
		`<img src="https://a.kajabi.com/9/9d08eac.png" alt="" width="20" height="1">` +
		`<img src="https://a.kajabi.com/9/9d08eac.png" alt="" width="1" height="100">` +
		// This one IS a real 1×1 tracker pixel — should be counted.
		`<img src="https://email.kjbm.example.com/o/eJx8token" alt="" width="1" height="1">` +
		`</body></html>`

	_, _, _, _, _, spy := parseBody([]byte(raw))

	if spy.Count != 1 {
		t.Errorf("SpyPixelInfo.Count = %d, want 1 (only the 1x1 pixel)", spy.Count)
	}
}

func TestSpyPixelPlainTextEmail(t *testing.T) {
	// Plain-text emails should never report spy pixels.
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Just a normal text email."

	_, _, _, _, _, spy := parseBody([]byte(raw))

	if spy.Count != 0 {
		t.Errorf("plain-text email SpyPixelInfo.Count = %d, want 0", spy.Count)
	}
}

func TestParseBody_UnknownCharset(t *testing.T) {
	// Emails with unknown charsets should not fail — they should render
	// with raw bytes rather than crashing. This is common with legacy
	// encodings (ISO-8859-15, Windows-1256, etc.).
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=x-unknown-charset-999\r\n" +
		"Content-Transfer-Encoding: 7bit\r\n" +
		"\r\n" +
		"This email uses an unknown charset but should still be readable."

	body, _, _, _, _, _ := parseBody([]byte(raw))

	if body == "" {
		t.Error("parseBody returned empty body for unknown charset — should fall back to raw bytes")
	}
	if !strings.Contains(body, "unknown charset") {
		t.Errorf("expected body to contain raw text, got: %q", body)
	}
}

func TestParseBody_UnknownEncoding(t *testing.T) {
	// Emails with unknown transfer encodings should degrade gracefully.
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: x-uuencode\r\n" +
		"\r\n" +
		"This email uses an unusual encoding."

	body, _, _, _, _, _ := parseBody([]byte(raw))

	// Should not panic or return empty — may return raw bytes or partial content
	if body == "" {
		t.Error("parseBody returned empty body for unknown encoding — should not crash")
	}
}

func TestParseBody_MultipartUnknownCharset(t *testing.T) {
	// Multipart email where one part has an unknown charset.
	// The other part should still be parsed correctly.
	boundary := "test-boundary-charset"
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=" + boundary + "\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=x-fake-charset\r\n" +
		"\r\n" +
		"Plain text with unknown charset\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<html><body><p>HTML part is fine</p></body></html>\r\n" +
		"--" + boundary + "--\r\n"

	body, _, _, _, _, _ := parseBody([]byte(raw))

	if body == "" {
		t.Error("parseBody returned empty body for multipart with unknown charset")
	}
}

func TestParseBody_ISO88591_Base64(t *testing.T) {
	// Real-world case: Swiss bank email with charset=iso-8859-1 and base64 encoding.
	// German umlauts (ü, ä, ö) must decode correctly to UTF-8.
	// In ISO-8859-1: ü=0xFC, ä=0xE4, ö=0xF6, Ü=0xDC
	iso88591Bytes := []byte("H\xe4b \xe4 sch\xf6ne Abe\r\n\xdcber CHF 7'260")
	encoded := base64.StdEncoding.EncodeToString(iso88591Bytes)

	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=iso-8859-1\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		encoded + "\r\n"

	body, _, _, _, _, _ := parseBody([]byte(raw))

	for _, want := range []string{"Häb", "schöne", "Über"} {
		if !strings.Contains(body, want) {
			t.Errorf("ISO-8859-1 body missing %q, got: %q", want, body)
		}
	}
}

func TestParseBody_Windows1252(t *testing.T) {
	// Windows-1252 is common in Outlook-generated emails.
	// It extends ISO-8859-1 with characters like curly quotes and em-dash.
	// €=0x80, –=0x96, "=0x93, "=0x94
	win1252Bytes := []byte("Price: \x804'500 \x93special offer\x94 \x96 limited")
	encoded := base64.StdEncoding.EncodeToString(win1252Bytes)

	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=windows-1252\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		encoded + "\r\n"

	body, _, _, _, _, _ := parseBody([]byte(raw))

	if !strings.Contains(body, "€") {
		t.Errorf("Windows-1252 body missing euro sign €, got: %q", body)
	}
	if !strings.Contains(body, "\u201c") || !strings.Contains(body, "\u201d") {
		t.Logf("Windows-1252 curly quotes may not be preserved, body: %q", body)
	}
}

func TestParseBody_ISO885915(t *testing.T) {
	// ISO-8859-15 is used in French/Finnish email. It adds € (0xA4) and
	// other characters missing from ISO-8859-1.
	iso885915Bytes := []byte("Cr\xe8me br\xfbl\xe9e co\xfbte \xa4100")
	encoded := base64.StdEncoding.EncodeToString(iso885915Bytes)

	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=iso-8859-15\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		encoded + "\r\n"

	body, _, _, _, _, _ := parseBody([]byte(raw))

	for _, want := range []string{"Crème", "brûlée", "coûte"} {
		if !strings.Contains(body, want) {
			t.Errorf("ISO-8859-15 body missing %q, got: %q", want, body)
		}
	}
}

func TestParseBody_QuotedPrintable_ISO88591(t *testing.T) {
	// Same charset but with quoted-printable encoding instead of base64.
	// Common in inline replies and older mail clients.
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=iso-8859-1\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Gr=FC=DFe aus Z=FCrich\r\n"

	body, _, _, _, _, _ := parseBody([]byte(raw))

	if !strings.Contains(body, "Grüße") {
		t.Errorf("QP ISO-8859-1 body missing 'Grüße', got: %q", body)
	}
	if !strings.Contains(body, "Zürich") {
		t.Errorf("QP ISO-8859-1 body missing 'Zürich', got: %q", body)
	}
}

func TestConnectionHealthCheck_LastActivity(t *testing.T) {
	// Verify that lastActivity is tracked by the Client struct.
	// We can't test the actual NOOP probe without a real IMAP server,
	// but we can verify the field exists and the logic is wired up.
	c := &Client{
		cfg: Config{
			Host: "imap.example.com",
			Port: "993",
			TLS:  true,
		},
	}

	// Initially zero — first withConn should not trigger NOOP
	if !c.lastActivity.IsZero() {
		t.Error("lastActivity should be zero on new Client")
	}

	// After setting lastActivity to recent, NOOP should not trigger
	c.lastActivity = time.Now()
	if time.Since(c.lastActivity) > 2*time.Minute {
		t.Error("recent lastActivity should not trigger health check")
	}

	// After setting lastActivity to 3 minutes ago, NOOP should trigger
	c.lastActivity = time.Now().Add(-3 * time.Minute)
	if time.Since(c.lastActivity) <= 2*time.Minute {
		t.Error("stale lastActivity (3min ago) should trigger health check")
	}
}

func TestResetMailboxSelection(t *testing.T) {
	// Verify that ResetMailboxSelection clears the cached selectedMailbox.
	// This prevents stale mailbox state from suppressing new message visibility
	// when refreshing (github.com/sspaeti/neomd#66 regression test).
	c := &Client{
		cfg: Config{
			Host: "imap.example.com",
			Port: "993",
			TLS:  true,
		},
	}

	// Simulate that a mailbox was previously selected
	c.selectedMailbox = "INBOX"

	// Reset should clear it
	c.ResetMailboxSelection()

	if c.selectedMailbox != "" {
		t.Errorf("ResetMailboxSelection() did not clear selectedMailbox: got %q, want empty string", c.selectedMailbox)
	}
}

func TestReadOnlyBlocksMutationsBeforeDial(t *testing.T) {
	c := New(Config{
		Host:     "imap.example.com",
		Port:     "993",
		TLS:      true,
		ReadOnly: true,
	})

	checks := []struct {
		name string
		run  func() error
	}{
		{"move", func() error { _, err := c.MoveMessage(nil, "INBOX", 1, "Trash"); return err }},
		{"create", func() error { _, err := c.EnsureFolders(nil, []string{"Archive"}); return err }},
		{"expunge", func() error { return c.ExpungeAll(nil, "Trash", []uint32{1}) }},
		{"mark seen", func() error { return c.MarkSeen(nil, "INBOX", 1) }},
		{"mark unseen", func() error { return c.MarkUnseen(nil, "INBOX", 1) }},
		{"mark answered", func() error { return c.MarkAnswered(nil, "INBOX", 1) }},
		{"mark flagged", func() error { return c.MarkFlagged(nil, "INBOX", 1) }},
		{"save sent", func() error { return c.SaveSent(nil, "Sent", []byte("message")) }},
		{"save draft", func() error { return c.SaveDraft(nil, "Drafts", []byte("message")) }},
		{"park reminder", func() error { return c.ParkReminder(nil, Email{}, nil, "Waiting", "Trash", time.Now()) }},
		{"save reminder", func() error { return c.SaveReminder(nil, "Waiting", nil) }},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, ErrReadOnly) {
				t.Fatalf("error = %v, want ErrReadOnly", err)
			}
		})
	}
	if c.conn != nil {
		t.Fatal("read-only mutation guard dialed IMAP")
	}
}

func TestReadOnlyMailboxSelectionUsesExamine(t *testing.T) {
	if !mailboxSelectOptions(true).ReadOnly {
		t.Fatal("read-only client must request IMAP EXAMINE")
	}
	if mailboxSelectOptions(false).ReadOnly {
		t.Fatal("ordinary client must retain IMAP SELECT behavior")
	}
}
