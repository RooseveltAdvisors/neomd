package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sspaeti/neomd/internal/config"
	goIMAP "github.com/sspaeti/neomd/internal/imap"
	"github.com/sspaeti/neomd/internal/link"
)

type agentReadOpts struct {
	messageID string
	folder    string
	account   string
	json      bool
	raw       bool
}

type readFolder struct {
	label string
	name  string
}

type readClient struct {
	account string
	client  readMessageFinder
}

type readMessageFinder interface {
	SearchMessageIDs(context.Context, string, string) ([]uint32, error)
	FetchRaw(context.Context, string, uint32) ([]byte, error)
}

type agentReadAttachment struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

type agentReadResult struct {
	OK          bool                  `json:"ok"`
	Found       bool                  `json:"found"`
	Account     string                `json:"account"`
	Folder      string                `json:"folder"`
	From        string                `json:"from"`
	To          string                `json:"to"`
	Cc          string                `json:"cc"`
	Date        string                `json:"date"`
	Subject     string                `json:"subject"`
	MessageID   string                `json:"message_id"`
	InReplyTo   string                `json:"in_reply_to"`
	References  string                `json:"references"`
	Body        string                `json:"body"`
	Attachments []agentReadAttachment `json:"attachments"`
	Error       string                `json:"error,omitempty"`
	Searched    []string              `json:"-"`
}

func parseAgentReadArgs(args []string) (agentReadOpts, error) {
	var opts agentReadOpts
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--json":
			opts.json = true
		case "--raw":
			opts.raw = true
		case "--folder", "-folder":
			value, next, err := readFlagValue(args, i, arg)
			if err != nil {
				return agentReadOpts{}, err
			}
			opts.folder, i = value, next
		case "--account", "-account":
			value, next, err := readFlagValue(args, i, arg)
			if err != nil {
				return agentReadOpts{}, err
			}
			opts.account, i = value, next
		case "--config", "-config":
			// Accepted here so callers can pass all options after `read`.
			if _, next, err := readFlagValue(args, i, arg); err != nil {
				return agentReadOpts{}, err
			} else {
				i = next
			}
		default:
			if strings.HasPrefix(arg, "--folder=") {
				opts.folder = strings.TrimPrefix(arg, "--folder=")
			} else if strings.HasPrefix(arg, "--account=") {
				opts.account = strings.TrimPrefix(arg, "--account=")
			} else if strings.HasPrefix(arg, "--config=") {
				// The main command extracts this before global flag parsing.
			} else if strings.HasPrefix(arg, "-") {
				return agentReadOpts{}, fmt.Errorf("unknown read option %q", arg)
			} else if opts.messageID == "" {
				opts.messageID = arg
			} else {
				return agentReadOpts{}, fmt.Errorf("read accepts one Message-ID")
			}
		}
	}
	if opts.json && opts.raw {
		return agentReadOpts{}, fmt.Errorf("--json and --raw cannot be combined")
	}
	return opts, nil
}

func readFlagValue(args []string, index int, flagName string) (string, int, error) {
	if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
		return "", index, fmt.Errorf("%s requires a value", flagName)
	}
	return args[index+1], index + 1, nil
}

func configuredReadFolders(f config.FoldersConfig) []readFolder {
	all := []readFolder{
		{"Inbox", f.Inbox}, {"ToScreen", f.ToScreen}, {"Feed", f.Feed},
		{"PaperTrail", f.PaperTrail}, {"Waiting", f.Waiting}, {"Someday", f.Someday},
		{"Scheduled", f.Scheduled}, {"Sent", f.Sent}, {"Archive", f.Archive},
		{"ScreenedOut", f.ScreenedOut}, {"Drafts", f.Drafts}, {"Trash", f.Trash},
		{"Spam", f.Spam}, {"Work", f.Work},
	}
	seen := make(map[string]bool, len(all))
	result := make([]readFolder, 0, len(all))
	for _, folder := range all {
		if folder.name == "" || seen[strings.ToLower(folder.name)] {
			continue
		}
		seen[strings.ToLower(folder.name)] = true
		result = append(result, folder)
	}
	return result
}

func orderedReadFolders(f config.FoldersConfig, hint string) []readFolder {
	all := configuredReadFolders(f)
	if hint == "" {
		return all
	}
	hint = strings.TrimSpace(hint)
	for i, folder := range all {
		if strings.EqualFold(hint, folder.label) || strings.EqualFold(hint, folder.name) ||
			strings.EqualFold(strings.ReplaceAll(hint, "_", ""), strings.ReplaceAll(folder.label, "_", "")) {
			return append([]readFolder{folder}, append(all[:i], all[i+1:]...)...)
		}
	}
	return all
}

func bareMessageID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") {
		return id[1 : len(id)-1]
	}
	return id
}

func searchAgentMessage(ctx context.Context, folders config.FoldersConfig, clients []readClient, inputID, hint string) (agentReadResult, []byte, bool, error) {
	searchID, uriFolder, err := link.ParseMessageIDInput(inputID)
	if err != nil {
		return agentReadResult{}, nil, false, err
	}
	if hint == "" {
		hint = uriFolder
	}
	var searched []string
	var lastErr error
	anySuccessfulSearch := false
	for _, account := range clients {
		for _, folder := range orderedReadFolders(folders, hint) {
			searched = append(searched, account.account+":"+folder.label)
			uids, searchErr := account.client.SearchMessageIDs(ctx, folder.name, searchID)
			if searchErr != nil {
				lastErr = searchErr
				continue
			}
			anySuccessfulSearch = true
			for _, uid := range uids {
				raw, fetchErr := account.client.FetchRaw(ctx, folder.name, uid)
				if fetchErr != nil {
					lastErr = fetchErr
					continue
				}
				message, body, attachments, parseErr := goIMAP.ParseRawMessage(raw)
				if parseErr != nil {
					lastErr = parseErr
					continue
				}
				if !strings.EqualFold(bareMessageID(message.MessageID), bareMessageID(searchID)) {
					continue
				}
				return makeAgentReadResult(account.account, folder.label, message, body, attachments, searched), raw, true, nil
			}
		}
	}
	if !anySuccessfulSearch && lastErr != nil {
		return agentReadResult{Searched: searched}, nil, false, lastErr
	}
	return agentReadResult{Searched: searched}, nil, false, nil
}

func makeAgentReadResult(account, folder string, message goIMAP.Email, body string, attachments []goIMAP.Attachment, searched []string) agentReadResult {
	result := agentReadResult{
		OK: true, Found: true, Account: account, Folder: folder,
		From: message.From, To: message.To, Cc: message.CC,
		Subject: message.Subject, MessageID: message.MessageID,
		InReplyTo: message.InReplyTo, References: message.References,
		Body: body, Searched: searched, Attachments: make([]agentReadAttachment, 0, len(attachments)),
	}
	if !message.Date.IsZero() {
		result.Date = message.Date.Format(time.RFC3339)
	}
	for _, attachment := range attachments {
		result.Attachments = append(result.Attachments, agentReadAttachment{
			Name: attachment.Filename, Type: attachment.ContentType, Size: len(attachment.Data),
		})
	}
	return result
}

func runAgentRead(ctx context.Context, folders config.FoldersConfig, clients []readClient, opts agentReadOpts, stdin io.Reader, stdout, stderr io.Writer) int {
	var inputs []string
	if opts.messageID == "" {
		scanner := bufio.NewScanner(stdin)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				inputs = append(inputs, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return emitAgentReadError(opts, stdout, stderr, 2, err)
		}
		if len(inputs) == 0 {
			return emitAgentReadError(opts, stdout, stderr, 1, fmt.Errorf("no Message-ID supplied"))
		}
	} else {
		inputs = []string{opts.messageID}
	}

	status := 0
	for _, input := range inputs {
		result, raw, found, err := searchAgentMessage(ctx, folders, clients, input, opts.folder)
		if err != nil {
			status = maxReadStatus(status, 2)
			result.Error = err.Error()
			if opts.json {
				writeAgentReadJSON(stdout, result)
			} else {
				fmt.Fprintf(stderr, "neomd read: %s: %v\n", input, err)
			}
			continue
		}
		if !found {
			status = maxReadStatus(status, 1)
			result.Error = "message not found"
			if opts.json {
				writeAgentReadJSON(stdout, result)
			} else {
				fmt.Fprintf(stderr, "neomd read: %s: message not found; searched %s\n", input, strings.Join(result.Searched, ", "))
			}
			continue
		}
		if opts.raw {
			if _, err := stdout.Write(raw); err != nil {
				return 2
			}
			continue
		}
		if opts.json {
			writeAgentReadJSON(stdout, result)
		} else {
			writeAgentReadText(stdout, result)
		}
	}
	return status
}

func maxReadStatus(current, next int) int {
	if next > current {
		return next
	}
	return current
}

func emitAgentReadError(opts agentReadOpts, stdout, stderr io.Writer, status int, err error) int {
	if opts.json {
		writeAgentReadJSON(stdout, agentReadResult{Error: err.Error()})
	} else {
		fmt.Fprintf(stderr, "neomd read: %v\n", err)
	}
	return status
}

func writeAgentReadJSON(w io.Writer, result agentReadResult) {
	_ = json.NewEncoder(w).Encode(result)
}

func writeAgentReadText(w io.Writer, result agentReadResult) {
	fmt.Fprintf(w, "From: %s\nTo: %s\n", result.From, result.To)
	fmt.Fprintf(w, "Cc: %s\nDate: %s\nSubject: %s\nMessage-ID: %s\nFolder: %s\nAccount: %s\n",
		result.Cc,
		result.Date, result.Subject, result.MessageID, result.Folder, result.Account)
	if result.InReplyTo != "" {
		fmt.Fprintf(w, "In-Reply-To: %s\n", result.InReplyTo)
	}
	if result.References != "" {
		fmt.Fprintf(w, "References: %s\n", result.References)
	}
	fmt.Fprintf(w, "\n%s\n", result.Body)
	if len(result.Attachments) > 0 {
		fmt.Fprintln(w, "Attachments:")
		for _, attachment := range result.Attachments {
			fmt.Fprintf(w, "- %s (%s, %d bytes)\n", attachment.Name, attachment.Type, attachment.Size)
		}
	}
}
