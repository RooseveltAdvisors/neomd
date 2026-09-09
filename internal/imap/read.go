package imap

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	mailattachments "github.com/sspaeti/neomd/internal/attachments"
)

// ParseRawMessage parses an RFC822 message for the agent-facing read command.
// Unlike parseBody, plain text is preferred over HTML because it is the most
// faithful representation for a shell consumer. HTML falls back to the same
// Markdown conversion used by the TUI reader.
func ParseRawMessage(raw []byte) (Email, string, []Attachment, error) {
	e, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return Email{}, "", nil, fmt.Errorf("parse message: %w", err)
	}
	if e == nil {
		return Email{}, "", nil, fmt.Errorf("parse message: empty entity")
	}

	mr := mail.NewReader(e)
	email := Email{
		From:       formatReadAddresses(mustAddressList(&mr.Header, "From")),
		To:         formatReadAddresses(mustAddressList(&mr.Header, "To")),
		CC:         formatReadAddresses(mustAddressList(&mr.Header, "Cc")),
		BCC:        formatReadAddresses(mustAddressList(&mr.Header, "Bcc")),
		MessageID:  strings.TrimSpace(mr.Header.Get("Message-ID")),
		InReplyTo:  strings.TrimSpace(mr.Header.Get("In-Reply-To")),
		References: strings.TrimSpace(mr.Header.Get("References")),
	}
	if subject, subjectErr := mr.Header.Subject(); subjectErr == nil {
		email.Subject = subject
	} else {
		email.Subject = mr.Header.Get("Subject")
	}
	if date, dateErr := mr.Header.Date(); dateErr == nil {
		email.Date = date
	}

	var plainText, htmlText string
	var attachments []Attachment
	for {
		part, partErr := mr.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil && !message.IsUnknownCharset(partErr) && !message.IsUnknownEncoding(partErr) {
			return email, "", nil, fmt.Errorf("parse message part: %w", partErr)
		}
		if part == nil {
			continue
		}

		var contentType string
		var filename string
		switch header := part.Header.(type) {
		case *mail.InlineHeader:
			params, _ := readContentType(header)
			contentType = params.contentType
			filename = params.name
		case *mail.AttachmentHeader:
			contentType, _, _ = header.ContentType()
			filename, _ = header.Filename()
			data, readErr := mailattachments.ReadAll(part.Body)
			if readErr != nil {
				return email, "", nil, fmt.Errorf("read attachment %q: %w", filename, readErr)
			}
			if filename != "" {
				attachments = append(attachments, Attachment{
					Filename: filename, ContentType: contentType, Data: data,
					IsCalendarInvite: isCalendarPart(contentType, filename),
				})
			}
			continue
		}

		data, readErr := mailattachments.ReadAll(part.Body)
		if readErr != nil {
			return email, "", nil, fmt.Errorf("read message body: %w", readErr)
		}
		switch strings.ToLower(contentType) {
		case "text/plain":
			if plainText == "" {
				plainText = string(data)
			}
		case "text/html":
			if htmlText == "" {
				htmlText = string(data)
			}
		default:
			// Inline images and other non-text parts are useful in the
			// attachment list even when their disposition is inline.
			if contentType != "" {
				if filename == "" {
					filename = fallbackAttachmentName(contentType)
				}
				attachments = append(attachments, Attachment{
					Filename: filename, ContentType: contentType, Data: data,
					IsCalendarInvite: isCalendarPart(contentType, filename),
				})
			}
		}
	}

	if plainText != "" {
		return email, strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(plainText, "\r\n", "\n"), "\r", "\n")), attachments, nil
	}
	if htmlText != "" {
		body, _ := htmlToMarkdown(htmlText)
		return email, body, attachments, nil
	}
	return email, "(no body)", attachments, nil
}

type readContentTypeResult struct {
	contentType string
	name        string
}

func readContentType(header *mail.InlineHeader) (readContentTypeResult, error) {
	contentType, params, err := header.ContentType()
	return readContentTypeResult{contentType: contentType, name: params["name"]}, err
}

func mustAddressList(header *mail.Header, key string) []*mail.Address {
	addresses, _ := header.AddressList(key)
	return addresses
}

func formatReadAddresses(addresses []*mail.Address) string {
	if len(addresses) == 0 {
		return ""
	}
	parts := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address.Name == "" || strings.ContainsAny(address.Name, `,<>")`) {
			parts = append(parts, address.Address)
			continue
		}
		parts = append(parts, address.Name+" <"+address.Address+">")
	}
	return strings.Join(parts, ", ")
}

func fallbackAttachmentName(contentType string) string {
	parts := strings.SplitN(contentType, "/", 2)
	if len(parts) == 2 && parts[1] != "" {
		return "attachment." + parts[1]
	}
	return "attachment.bin"
}
