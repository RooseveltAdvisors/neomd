// Package link contains neomd's shareable link formats.
package link

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	Scheme        = "neomd"
	MessageIDHost = "mid"
)

// MessageIDURI returns the canonical neomd URI for an RFC 5322 Message-ID.
// The ID is one URL path segment so characters such as angle brackets, spaces,
// question marks, and slashes cannot change the URI's structure.
func MessageIDURI(messageID string) (string, error) {
	if strings.TrimSpace(messageID) == "" {
		return "", fmt.Errorf("message-id is empty")
	}
	return Scheme + "://" + MessageIDHost + "/" + url.PathEscape(messageID), nil
}

// ParseMessageIDURI decodes a neomd Message-ID URI and rejects other URI
// shapes. The returned ID is byte-for-byte equivalent to the encoded value.
func ParseMessageIDURI(raw string) (string, error) {
	id, _, err := ParseMessageIDURIWithFolder(raw)
	return id, err
}

// ParseMessageIDURIWithFolder decodes a neomd Message-ID URI and its optional
// folder hint. The returned ID is byte-for-byte equivalent to the encoded
// value. The only supported query parameter is folder.
func ParseMessageIDURIWithFolder(raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse neomd URI: %w", err)
	}
	if !strings.EqualFold(u.Scheme, Scheme) || !strings.EqualFold(u.Host, MessageIDHost) || u.Fragment != "" {
		return "", "", fmt.Errorf("unsupported neomd URI %q", raw)
	}
	var folder string
	if u.RawQuery != "" {
		values, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "", "", fmt.Errorf("parse neomd URI query: %w", err)
		}
		for key := range values {
			if key != "folder" {
				return "", "", fmt.Errorf("unsupported neomd URI query parameter %q", key)
			}
		}
		if len(values["folder"]) > 1 {
			return "", "", fmt.Errorf("neomd URI has duplicate folder parameters")
		}
		folder = values.Get("folder")
	}

	escaped := strings.TrimPrefix(u.EscapedPath(), "/")
	if escaped == "" || strings.Contains(escaped, "/") {
		return "", "", fmt.Errorf("neomd Message-ID URI must contain one path segment")
	}
	id, err := url.PathUnescape(escaped)
	if err != nil {
		return "", "", fmt.Errorf("decode Message-ID URI: %w", err)
	}
	if id == "" || strings.TrimSpace(id) != id {
		return "", "", fmt.Errorf("decoded Message-ID is empty or has surrounding whitespace")
	}
	return id, folder, nil
}

// ParseMessageIDInput accepts a neomd Message-ID URI or a bare RFC Message-ID.
// Bare IDs may optionally include their angle brackets. The returned ID is
// normalized to the bare form used for exact comparisons.
func ParseMessageIDInput(raw string) (id, folder string, err error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), Scheme+"://") {
		id, folder, err = ParseMessageIDURIWithFolder(raw)
		if err != nil {
			return "", "", err
		}
	} else {
		id = raw
	}
	if strings.HasPrefix(id, "<") || strings.HasSuffix(id, ">") {
		if len(id) < 2 || !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, ">") {
			return "", "", fmt.Errorf("malformed bracketed Message-ID")
		}
		id = id[1 : len(id)-1]
	}
	if id == "" || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\r\n") {
		return "", "", fmt.Errorf("Message-ID is empty or invalid")
	}
	return id, folder, nil
}
