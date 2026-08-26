package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sspaeti/neomd/internal/config"
	goIMAP "github.com/sspaeti/neomd/internal/imap"
)

func testFolders() config.FoldersConfig {
	return config.FoldersConfig{
		Inbox:      "INBOX",
		ToScreen:   "ToScreen",
		Feed:       "Feed",
		PaperTrail: "HEY/Paper Trail",
	}
}

func TestParseListArgs_Defaults(t *testing.T) {
	opts, err := parseListArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Inbox", "ToScreen", "Feed", "PaperTrail"}
	if len(opts.folders) != len(want) {
		t.Fatalf("folders = %v, want %v", opts.folders, want)
	}
	for i := range want {
		if opts.folders[i] != want[i] {
			t.Errorf("folders[%d] = %q, want %q", i, opts.folders[i], want[i])
		}
	}
	if opts.limit != 15 {
		t.Errorf("limit = %d, want 15", opts.limit)
	}
}

func TestParseListArgs_Custom(t *testing.T) {
	opts, err := parseListArgs([]string{"--folders", "Inbox,Feed", "--limit", "5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts.folders) != 2 || opts.folders[0] != "Inbox" || opts.folders[1] != "Feed" {
		t.Errorf("folders = %v, want [Inbox Feed]", opts.folders)
	}
	if opts.limit != 5 {
		t.Errorf("limit = %d, want 5", opts.limit)
	}
}

func TestParseListArgs_BadFlag(t *testing.T) {
	if _, err := parseListArgs([]string{"--nope"}); err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

func TestResolveListFolder(t *testing.T) {
	f := testFolders()
	cases := []struct {
		in       string
		imapName string
		label    string
		ok       bool
	}{
		{"Inbox", "INBOX", "Inbox", true},
		{"inbox", "INBOX", "Inbox", true},
		{"ToScreen", "ToScreen", "ToScreen", true},
		{"to_screen", "ToScreen", "ToScreen", true},
		{"papertrail", "HEY/Paper Trail", "PaperTrail", true},
		{"PaperTrail", "HEY/Paper Trail", "PaperTrail", true},
		{"feed", "Feed", "Feed", true},
		{"Bogus", "", "", false},
	}
	for _, c := range cases {
		imapName, label, ok := resolveListFolder(f, c.in)
		if ok != c.ok || imapName != c.imapName || label != c.label {
			t.Errorf("resolveListFolder(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.in, imapName, label, ok, c.imapName, c.label, c.ok)
		}
	}
}

type fakeFetcher struct {
	byFolder map[string][]goIMAP.Email
	err      error
	requests []string
}

func (f *fakeFetcher) FetchHeaders(_ context.Context, folder string, _ int) ([]goIMAP.Email, error) {
	f.requests = append(f.requests, folder)
	if f.err != nil {
		return nil, f.err
	}
	return f.byFolder[folder], nil
}

func TestRunList_JSONShape(t *testing.T) {
	date := time.Date(2026, 8, 25, 10, 30, 0, 0, time.UTC)
	fetcher := &fakeFetcher{byFolder: map[string][]goIMAP.Email{
		"ToScreen": {
			{UID: 42, From: "Jane <jane@example.com>", Subject: "Hello", Date: date, Seen: false},
			{UID: 41, From: "bob@example.com", Subject: "Old news", Date: date.Add(-time.Hour), Seen: true},
		},
	}}
	var buf bytes.Buffer
	code := runList(context.Background(), testFolders(), "Personal", fetcher,
		[]string{"--folders", "ToScreen", "--limit", "10"}, &buf)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var out struct {
		OK      bool   `json:"ok"`
		Account string `json:"account"`
		Folders []struct {
			Name   string `json:"name"`
			Emails []struct {
				UID     uint32 `json:"uid"`
				From    string `json:"from"`
				Subject string `json:"subject"`
				Date    string `json:"date"`
				Unread  bool   `json:"unread"`
			} `json:"emails"`
		} `json:"folders"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if !out.OK {
		t.Fatal("ok = false, want true")
	}
	if out.Account != "Personal" {
		t.Errorf("account = %q, want Personal", out.Account)
	}
	if len(out.Folders) != 1 || out.Folders[0].Name != "ToScreen" {
		t.Fatalf("folders = %+v, want one ToScreen", out.Folders)
	}
	emails := out.Folders[0].Emails
	if len(emails) != 2 {
		t.Fatalf("emails = %d, want 2", len(emails))
	}
	if emails[0].UID != 42 || emails[0].From != "Jane <jane@example.com>" ||
		emails[0].Subject != "Hello" || !emails[0].Unread {
		t.Errorf("first email wrong: %+v", emails[0])
	}
	if emails[1].Unread {
		t.Error("second email unread = true, want false (Seen)")
	}
	if emails[0].Date != "2026-08-25T10:30:00Z" {
		t.Errorf("date = %q, want RFC3339 UTC", emails[0].Date)
	}
}

func TestRunList_TruncatesHostileHeaders(t *testing.T) {
	huge := strings.Repeat("A", 100_000)
	fetcher := &fakeFetcher{byFolder: map[string][]goIMAP.Email{
		"Feed": {{UID: 1, From: huge, Subject: huge, Date: time.Now()}},
	}}
	var buf bytes.Buffer
	runList(context.Background(), testFolders(), "Personal", fetcher,
		[]string{"--folders", "Feed"}, &buf)
	var out listOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	e := out.Folders[0].Emails[0]
	if len(e.From) > listHeaderMaxBytes || len(e.Subject) > listHeaderMaxBytes {
		t.Errorf("headers not truncated: from=%d subject=%d bytes (max %d)",
			len(e.From), len(e.Subject), listHeaderMaxBytes)
	}
}

func TestRunList_EmptyFolderIsArrayNotNull(t *testing.T) {
	fetcher := &fakeFetcher{byFolder: map[string][]goIMAP.Email{}}
	var buf bytes.Buffer
	runList(context.Background(), testFolders(), "Personal", fetcher,
		[]string{"--folders", "Feed"}, &buf)
	if bytes.Contains(buf.Bytes(), []byte(`"emails":null`)) {
		t.Fatalf("emails is null, want []: %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"emails":[]`)) {
		t.Fatalf("expected empty emails array: %s", buf.String())
	}
}

func TestRunList_ResolvesConfiguredIMAPName(t *testing.T) {
	fetcher := &fakeFetcher{byFolder: map[string][]goIMAP.Email{}}
	var buf bytes.Buffer
	runList(context.Background(), testFolders(), "Personal", fetcher,
		[]string{"--folders", "papertrail"}, &buf)
	if len(fetcher.requests) != 1 || fetcher.requests[0] != "HEY/Paper Trail" {
		t.Fatalf("fetched %v, want [HEY/Paper Trail]", fetcher.requests)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"name":"PaperTrail"`)) {
		t.Fatalf("output should use label PaperTrail: %s", buf.String())
	}
}

func TestRunList_FetchErrorJSON(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.New("connection refused")}
	var buf bytes.Buffer
	code := runList(context.Background(), testFolders(), "Personal", fetcher,
		[]string{"--folders", "Inbox"}, &buf)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (widget needs JSON, not a crash)", code)
	}
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if out.OK {
		t.Fatal("ok = true, want false")
	}
	if out.Error == "" {
		t.Fatal("error message empty")
	}
}

func TestRunList_UnknownFolderErrorJSON(t *testing.T) {
	fetcher := &fakeFetcher{}
	var buf bytes.Buffer
	code := runList(context.Background(), testFolders(), "Personal", fetcher,
		[]string{"--folders", "Bogus"}, &buf)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if out.OK || out.Error == "" {
		t.Fatalf("want ok=false with error, got %+v", out)
	}
}
