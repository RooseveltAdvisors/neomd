package search

import (
	"testing"
	"time"

	"github.com/sspaeti/neomd/internal/imap"
)

func doc(uid uint32, from, subject string) imap.Email {
	return imap.Email{Account: "fixture", Folder: "Inbox", UID: uid, From: from, Subject: subject, Date: time.Unix(int64(uid), 0)}
}

func TestSearchMatchesHeadersBodyNamesAndTypos(t *testing.T) {
	i := New()
	e := doc(1, "Known Person <known@example.test>", "Quarterly report")
	i.UpsertHeader(e, "known person")
	i.UpsertBody(e, "known person", "The body contains a private project keyword.", "<p>HTML fallback term</p>")
	i.UpsertHeader(doc(2, "other@example.test", "Unrelated"), "")

	for _, tc := range []struct {
		name, query string
	}{
		{"name", "known"},
		{"prefix", "proj"},
		{"body html", "fallback"},
		{"case", "KNOWN"},
		{"typo", "projec"},
		{"subject", "subject:quarter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := i.Search(tc.query)
			if len(got) != 1 || got[0].UID != 1 {
				t.Fatalf("Search(%q) = %#v, want fixture uid 1", tc.query, got)
			}
		})
	}
}

func TestSearchMultiwordIsANDAndFiltersCanCompose(t *testing.T) {
	i := New()
	a := doc(1, "Alpha One <a@example.test>", "Project status")
	b := doc(2, "Beta Two <b@example.test>", "Project status")
	i.UpsertBody(a, "", "alpha one planning", "")
	i.UpsertBody(b, "", "beta two planning", "")
	got := i.Search("from:alpha project")
	if len(got) != 1 || got[0].UID != 1 {
		t.Fatalf("composed query = %#v, want uid 1", got)
	}
	if got := i.Search("alpha missing"); len(got) != 0 {
		t.Fatalf("AND query returned %#v", got)
	}
}

func TestIndexRefreshRetainsAndDeletesBySuccessfulScope(t *testing.T) {
	i := New()
	a := doc(1, "a@example.test", "old")
	b := doc(2, "b@example.test", "keep")
	i.UpsertBody(a, "", "old body", "")
	i.UpsertBody(b, "", "keep body", "")
	present := map[string]struct{}{Key(b): {}}
	i.RemoveMissing(present, map[string]struct{}{"fixture\x00Inbox": {}})
	if got := i.Search("old"); len(got) != 0 {
		t.Fatalf("deleted document remained: %#v", got)
	}
	if got := i.Search("keep"); len(got) != 1 {
		t.Fatalf("kept document missing: %#v", got)
	}
}
