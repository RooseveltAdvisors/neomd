package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/sspaeti/neomd/internal/config"
	goIMAP "github.com/sspaeti/neomd/internal/imap"
	"github.com/sspaeti/neomd/internal/screener"
)

// The screen subcommand classifies a sender from an external widget (e.g. the
// omarchy bar plugin) the same way the TUI's I/O/F/P keys do: update the
// screener lists, then move ALL queued ToScreen mail from that sender to the
// destination folder (sender-level classify invariant, see AGENTS.md).
// Output is always one JSON object; failures print {"ok":false,...} exit 0.

type screenOpts struct {
	from   string
	action string // in | out | feed | paper
}

type screenOutput struct {
	OK     bool   `json:"ok"`
	Action string `json:"action,omitempty"`
	From   string `json:"from,omitempty"`
	Moved  int    `json:"moved"`
	Error  string `json:"error,omitempty"`
}

// screenerOps is the slice of *screener.Screener that runScreen needs.
type screenerOps interface {
	Approve(from string) error
	Block(from string) error
	MarkFeed(from string) error
	MarkPaperTrail(from string) error
}

// screenIMAP is the slice of *imap.Client that runScreen needs.
type screenIMAP interface {
	SearchUIDs(ctx context.Context, folder string) ([]uint32, error)
	FetchHeadersByUID(ctx context.Context, folder string, uids []uint32) ([]goIMAP.Email, error)
	MoveMessage(ctx context.Context, src string, uid uint32, dst string) (uint32, error)
}

func parseScreenArgs(args []string) (screenOpts, error) {
	fs := flag.NewFlagSet("screen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	from := fs.String("from", "", "sender (raw From header or address)")
	action := fs.String("action", "", "in | out | feed | paper")
	if err := fs.Parse(args); err != nil {
		return screenOpts{}, err
	}
	if *from == "" {
		return screenOpts{}, fmt.Errorf("--from is required")
	}
	switch *action {
	case "in", "out", "feed", "paper":
	default:
		return screenOpts{}, fmt.Errorf("--action must be in|out|feed|paper, got %q", *action)
	}
	return screenOpts{from: *from, action: *action}, nil
}

// screenSenderAddr mirrors ui.normalizedSender/extractEmailAddr (unexported
// there): lowercase the bare address out of a "Name <addr>" From header.
func screenSenderAddr(from string) string {
	s := from
	if i := strings.IndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s, '>'); j > i {
			s = s[i+1 : j]
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func runScreen(ctx context.Context, folders config.FoldersConfig, sc screenerOps, cli screenIMAP, args []string, w io.Writer) int {
	opts, err := parseScreenArgs(args)
	if err != nil {
		return writeScreenJSON(w, screenOutput{Error: err.Error()})
	}

	// Same safety gate as the TUI: screener destinations may never be Trash.
	if err := screener.ValidateScreenerSafety(folders); err != nil {
		return writeScreenJSON(w, screenOutput{Error: err.Error()})
	}

	var classify func(string) error
	var dst string
	switch opts.action {
	case "in":
		classify, dst = sc.Approve, folders.Inbox
	case "out":
		classify, dst = sc.Block, folders.ScreenedOut
	case "feed":
		classify, dst = sc.MarkFeed, folders.Feed
	case "paper":
		classify, dst = sc.MarkPaperTrail, folders.PaperTrail
	}

	// List update first — if it fails, no mail moves (mirrors TUI order).
	if err := classify(opts.from); err != nil {
		return writeScreenJSON(w, screenOutput{Error: err.Error()})
	}

	// Sender-level move: every queued ToScreen message from this sender.
	sender := screenSenderAddr(opts.from)
	uids, err := cli.SearchUIDs(ctx, folders.ToScreen)
	if err != nil {
		return writeScreenJSON(w, screenOutput{Error: fmt.Sprintf("search %s: %v", folders.ToScreen, err)})
	}
	moved := 0
	for start := 0; start < len(uids); start += 200 {
		end := start + 200
		if end > len(uids) {
			end = len(uids)
		}
		batch, err := cli.FetchHeadersByUID(ctx, folders.ToScreen, uids[start:end])
		if err != nil {
			return writeScreenJSON(w, screenOutput{Moved: moved, Error: fmt.Sprintf("fetch %s: %v", folders.ToScreen, err)})
		}
		for _, e := range batch {
			if screenSenderAddr(e.From) != sender {
				continue
			}
			if _, err := cli.MoveMessage(ctx, folders.ToScreen, e.UID, dst); err != nil {
				return writeScreenJSON(w, screenOutput{Moved: moved, Error: fmt.Sprintf("move uid %d → %s: %v", e.UID, dst, err)})
			}
			moved++
		}
	}
	return writeScreenJSON(w, screenOutput{OK: true, Action: opts.action, From: opts.from, Moved: moved})
}

// writeScreenJSON always exits 0: errors are reported inside the JSON payload.
func writeScreenJSON(w io.Writer, out screenOutput) int {
	enc := json.NewEncoder(w)
	_ = enc.Encode(out)
	return 0
}
