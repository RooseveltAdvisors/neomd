package link

import "testing"

func TestMessageIDURIRoundTrip(t *testing.T) {
	for _, id := range []string{
		"<abc@example.com>",
		"<reply with spaces/and?markers@example.com>",
	} {
		uri, err := MessageIDURI(id)
		if err != nil {
			t.Fatalf("MessageIDURI(%q): %v", id, err)
		}
		if uri[:len(Scheme)+3] != Scheme+"://" {
			t.Fatalf("MessageIDURI(%q) = %q, missing neomd scheme", id, uri)
		}
		got, err := ParseMessageIDURI(uri)
		if err != nil {
			t.Fatalf("ParseMessageIDURI(%q): %v", uri, err)
		}
		if got != id {
			t.Errorf("ParseMessageIDURI(%q) = %q, want %q", uri, got, id)
		}
	}
}

func TestMessageIDURIRejectsInvalidValues(t *testing.T) {
	if _, err := MessageIDURI(" "); err == nil {
		t.Fatal("MessageIDURI should reject an empty ID")
	}
	for _, raw := range []string{
		"https://example.com/%3Cabc%40example.com%3E",
		"neomd://mid/",
		"neomd://mid/%3Cabc%40example.com%3E?folder=INBOX",
		"neomd://mid/%ZZ",
	} {
		if _, err := ParseMessageIDURI(raw); err == nil {
			t.Errorf("ParseMessageIDURI(%q) accepted invalid URI", raw)
		}
	}
}
