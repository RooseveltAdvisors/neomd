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
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse neomd URI: %w", err)
	}
	if u.Scheme != Scheme || u.Host != MessageIDHost || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("unsupported neomd URI %q", raw)
	}

	escaped := strings.TrimPrefix(u.EscapedPath(), "/")
	if escaped == "" || strings.Contains(escaped, "/") {
		return "", fmt.Errorf("neomd Message-ID URI must contain one path segment")
	}
	id, err := url.PathUnescape(escaped)
	if err != nil {
		return "", fmt.Errorf("decode Message-ID URI: %w", err)
	}
	if id == "" || strings.TrimSpace(id) != id {
		return "", fmt.Errorf("decoded Message-ID is empty or has surrounding whitespace")
	}
	return id, nil
}
