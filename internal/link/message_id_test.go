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
		"neomd://mid/%ZZ",
	} {
		if _, err := ParseMessageIDURI(raw); err == nil {
			t.Errorf("ParseMessageIDURI(%q) accepted invalid URI", raw)
		}
	}
}

func TestMessageIDURIWithFolderHint(t *testing.T) {
	id, folder, err := ParseMessageIDURIWithFolder("neomd://mid/%3Cabc%40example.com%3E?folder=INBOX")
	if err != nil || id != "<abc@example.com>" || folder != "INBOX" {
		t.Fatalf("got id=%q folder=%q err=%v", id, folder, err)
	}
}

func TestParseMessageIDInput(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"bracketed", "<abc@example.com>", "abc@example.com"},
		{"bare", "abc@example.com", "abc@example.com"},
		{"encoded", "neomd://mid/abc%40example.com", "abc@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := ParseMessageIDInput(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("got %q err=%v, want %q", got, err, tc.want)
			}
		})
	}
	for _, input := range []string{"", "<abc@example.com", "abc@example.com>", "<a\nb@example.com>"} {
		if _, _, err := ParseMessageIDInput(input); err == nil {
			t.Errorf("ParseMessageIDInput(%q) accepted invalid input", input)
		}
	}
}
