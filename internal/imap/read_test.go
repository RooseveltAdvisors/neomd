package imap

import (
	"strings"
	"testing"
)

func TestParseRawMessagePrefersPlainAndReportsAttachments(t *testing.T) {
	raw := []byte("From: Alice <alice@example.com>\r\n" +
		"To: Agent <agent@example.com>\r\n" +
		"Cc: copy@example.com\r\n" +
		"Date: Mon, 07 Sep 2026 12:00:00 +0000\r\n" +
		"Subject: =?UTF-8?Q?Pr=C3=BCfung?=\r\n" +
		"Message-ID: <read@example.com>\r\n" +
		"References: <root@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=bound\r\n\r\n" +
		"--bound\r\nContent-Type: multipart/alternative; boundary=alt\r\n\r\n" +
		"--alt\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nplain body\r\n" +
		"--alt\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<b>html body</b>\r\n--alt--\r\n" +
		"--bound\r\nContent-Type: application/pdf; name=report.pdf\r\nContent-Disposition: attachment; filename=report.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\nAQID\r\n--bound--\r\n")
	email, body, attachments, err := ParseRawMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if email.Subject != "Prüfung" || email.MessageID != "<read@example.com>" || email.References != "<root@example.com>" {
		t.Fatalf("headers = %+v", email)
	}
	if email.From != "Alice <alice@example.com>" || email.To != "Agent <agent@example.com>" || email.CC != "copy@example.com" {
		t.Fatalf("addresses = from=%q to=%q cc=%q", email.From, email.To, email.CC)
	}
	if body != "plain body" {
		t.Errorf("body = %q, want plain body", body)
	}
	if len(attachments) != 1 || attachments[0].Filename != "report.pdf" || string(attachments[0].Data) != "\x01\x02\x03" {
		t.Fatalf("attachments = %+v", attachments)
	}
}

func TestParseRawMessageHTMLFallback(t *testing.T) {
	raw := []byte("Content-Type: text/html; charset=utf-8\r\n\r\n<p>Hello <strong>agent</strong></p>")
	_, body, _, err := ParseRawMessage(raw)
	if err != nil || !strings.Contains(body, "Hello") || !strings.Contains(body, "agent") {
		t.Fatalf("body=%q err=%v", body, err)
	}
}
