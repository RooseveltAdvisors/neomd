package search

import (
	"context"
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

func TestSearchMatchesAddressesAndIncrementalChanges(t *testing.T) {
	i := New()
	e := doc(7, "Sender Person <sender@example.test>", "Initial subject")
	e.To = "Recipient Person <recipient@example.test>"
	i.UpsertBody(e, "Sender Person Recipient Person", "stable body token", "")

	for _, query := range []string{"from:sender@example.test", "to:recipient@example.test", "recipient", "stable"} {
		if got := i.Search(query); len(got) != 1 || got[0].UID != e.UID {
			t.Fatalf("Search(%q) = %#v, want fixture uid %d", query, got, e.UID)
		}
	}

	changed := e
	changed.Subject = "Updated subject"
	i.UpsertHeader(changed, "Sender Person Recipient Person")
	if got := i.Search("stable"); len(got) != 0 {
		t.Fatalf("stale body remained after header refresh: %#v", got)
	}
	i.UpsertBody(changed, "Sender Person Recipient Person", "fresh body token", "")
	if got := i.Search("fresh"); len(got) != 1 || got[0].Subject != changed.Subject {
		t.Fatalf("fresh body search = %#v, want updated fixture", got)
	}
}

func TestSearchContextHonorsCancellation(t *testing.T) {
	i := New()
	i.UpsertBody(doc(1, "sender@example.test", "subject"), "", "body", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := i.SearchContext(ctx, "body"); len(got) != 0 {
		t.Fatalf("canceled search returned %#v", got)
	}
}
