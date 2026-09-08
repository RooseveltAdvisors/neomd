package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sspaeti/neomd/internal/config"
	goIMAP "github.com/sspaeti/neomd/internal/imap"
)

// The list subcommand is a read-only data source for external widgets (e.g.
// the omarchy bar plugin): fetch headers for a set of folders, print one JSON
// object, exit. Failures also print JSON and exit 0 — a widget that gets no
// JSON has nothing to show but a crash.

// listHeaderMaxBytes caps sender-controlled header fields so a hostile
// Subject/From can't blow up the JSON that widgets buffer whole in memory.
const listHeaderMaxBytes = 500

type listOpts struct {
	folders []string
	limit   int
}

type listEmail struct {
	UID     uint32 `json:"uid"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"` // RFC3339, UTC
	Unread  bool   `json:"unread"`
}

type listFolder struct {
	Name   string      `json:"name"`
	Emails []listEmail `json:"emails"`
}

type listOutput struct {
	OK      bool         `json:"ok"`
	Account string       `json:"account,omitempty"`
	Folders []listFolder `json:"folders,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// headerFetcher is the slice of *imap.Client that runList needs; an interface
// so tests can fake the network.
type headerFetcher interface {
	FetchHeaders(ctx context.Context, folder string, n int) ([]goIMAP.Email, error)
}

func parseListArgs(args []string) (listOpts, error) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	folders := fs.String("folders", "Inbox,ToScreen,Feed,PaperTrail", "comma-separated folder labels")
	limit := fs.Int("limit", 15, "max emails per folder")
	if err := fs.Parse(args); err != nil {
		return listOpts{}, err
	}
	var names []string
	for _, f := range strings.Split(*folders, ",") {
		if f = strings.TrimSpace(f); f != "" {
			names = append(names, f)
		}
	}
	return listOpts{folders: names, limit: *limit}, nil
}

// resolveListFolder maps a user-facing folder name (label like "ToScreen" or
// config key like "to_screen", case-insensitive) to the configured IMAP
// mailbox name plus its canonical label.
func resolveListFolder(f config.FoldersConfig, name string) (imapName, label string, ok bool) {
	switch strings.ToLower(strings.ReplaceAll(name, "_", "")) {
	case "inbox":
		return f.Inbox, "Inbox", true
	case "toscreen":
		return f.ToScreen, "ToScreen", true
	case "feed":
		return f.Feed, "Feed", true
	case "papertrail":
		return f.PaperTrail, "PaperTrail", true
	case "screenedout":
		return f.ScreenedOut, "ScreenedOut", true
	case "archive":
		return f.Archive, "Archive", true
	case "waiting", "reminder", "reminders":
		return f.Waiting, "Reminders", true
	case "someday":
		return f.Someday, "Someday", true
	case "scheduled":
		return f.Scheduled, "Scheduled", true
	case "sent":
		return f.Sent, "Sent", true
	case "drafts":
		return f.Drafts, "Drafts", true
	}
	return "", "", false
}

func runList(ctx context.Context, folders config.FoldersConfig, account string, fetcher headerFetcher, args []string, w io.Writer) int {
	opts, err := parseListArgs(args)
	if err != nil {
		return writeListJSON(w, listOutput{Error: err.Error()})
	}
	out := listOutput{OK: true, Account: account}
	for _, name := range opts.folders {
		imapName, label, ok := resolveListFolder(folders, name)
		if !ok {
			return writeListJSON(w, listOutput{Error: fmt.Sprintf("unknown folder: %s", name)})
		}
		emails, err := fetcher.FetchHeaders(ctx, imapName, opts.limit)
		if err != nil {
			return writeListJSON(w, listOutput{Error: fmt.Sprintf("%s: %v", label, err)})
		}
		lf := listFolder{Name: label, Emails: make([]listEmail, 0, len(emails))}
		for _, e := range emails {
			from, _ := truncateUTF8(e.From, listHeaderMaxBytes)
			subject, _ := truncateUTF8(e.Subject, listHeaderMaxBytes)
			lf.Emails = append(lf.Emails, listEmail{
				UID:     e.UID,
				From:    from,
				Subject: subject,
				Date:    e.Date.UTC().Format(time.RFC3339),
				Unread:  !e.Seen,
			})
		}
		out.Folders = append(out.Folders, lf)
	}
	return writeListJSON(w, out)
}

// writeListJSON always exits 0: errors are reported inside the JSON payload.
func writeListJSON(w io.Writer, out listOutput) int {
	enc := json.NewEncoder(w)
	_ = enc.Encode(out)
	return 0
}
