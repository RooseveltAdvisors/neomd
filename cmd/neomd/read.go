package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/sspaeti/neomd/internal/config"
	goIMAP "github.com/sspaeti/neomd/internal/imap"
)

// The read subcommand serves one message body to external widgets (the
// omarchy bar plugin's in-panel reader). Strictly read-only: FetchBody
// fetches with BODY.PEEK, so reading here never sets \Seen. Output is always
// one JSON object; failures print {"ok":false,...} and exit 0.

type readOpts struct {
	folder   string
	uid      uint32
	maxBytes int
}

type readOutput struct {
	OK        bool   `json:"ok"`
	Folder    string `json:"folder,omitempty"`
	UID       uint32 `json:"uid,omitempty"`
	Body      string `json:"body,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

// bodyFetcher is the slice of *imap.Client that runRead needs.
type bodyFetcher interface {
	FetchBody(ctx context.Context, folder string, uid uint32) (string, string, string, []goIMAP.Attachment, string, goIMAP.SpyPixelInfo, error)
}

func parseReadArgs(args []string) (readOpts, error) {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	folder := fs.String("folder", "", "folder label (e.g. Feed)")
	uid := fs.Uint("uid", 0, "message UID")
	maxBytes := fs.Int("max-bytes", 65536, "truncate the body after this many bytes")
	if err := fs.Parse(args); err != nil {
		return readOpts{}, err
	}
	if *folder == "" {
		return readOpts{}, fmt.Errorf("--folder is required")
	}
	if *uid == 0 {
		return readOpts{}, fmt.Errorf("--uid is required")
	}
	return readOpts{folder: *folder, uid: uint32(*uid), maxBytes: *maxBytes}, nil
}

func runRead(ctx context.Context, folders config.FoldersConfig, fetcher bodyFetcher, args []string, w io.Writer) int {
	opts, err := parseReadArgs(args)
	if err != nil {
		return writeReadJSON(w, readOutput{Error: err.Error()})
	}
	imapName, label, ok := resolveListFolder(folders, opts.folder)
	if !ok {
		return writeReadJSON(w, readOutput{Error: fmt.Sprintf("unknown folder: %s", opts.folder)})
	}
	markdown, _, _, _, _, _, err := fetcher.FetchBody(ctx, imapName, opts.uid)
	if err != nil {
		return writeReadJSON(w, readOutput{Error: fmt.Sprintf("%s uid %d: %v", label, opts.uid, err)})
	}
	body, truncated := truncateUTF8(markdown, opts.maxBytes)
	return writeReadJSON(w, readOutput{OK: true, Folder: label, UID: opts.uid, Body: body, Truncated: truncated})
}

// truncateUTF8 cuts s after at most max bytes without splitting a rune.
func truncateUTF8(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// writeReadJSON always exits 0: errors are reported inside the JSON payload.
func writeReadJSON(w io.Writer, out readOutput) int {
	enc := json.NewEncoder(w)
	_ = enc.Encode(out)
	return 0
}
