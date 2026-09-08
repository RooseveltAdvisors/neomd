package ui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/config"
	"github.com/sspaeti/neomd/internal/contacts"
	"github.com/sspaeti/neomd/internal/imap"
	fulltext "github.com/sspaeti/neomd/internal/search"
)

type universalSearchResultMsg struct {
	emails          []imap.Email
	query           string
	indexed, bodies int
	total           int
	failedScopes    int
	failedBodies    int
	canceled        bool
}

// searchFolders is the explicit, configured scope for universal search. Empty
// paths are omitted and duplicate aliases are visited once.
func (m Model) searchFolders() []string {
	if m.cfg == nil {
		return nil
	}
	f := m.cfg.Folders
	paths := []string{f.Inbox, f.Sent, f.Trash, f.Drafts, f.ToScreen, f.Feed, f.PaperTrail, f.ScreenedOut, f.Archive, f.Waiting, f.Scheduled, f.Someday, f.Spam, f.Work}
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func (m Model) canUniversalSearch() bool {
	return m.cfg != nil && m.searchIndex != nil && len(m.clients) > 0
}

func (m Model) universalSearchCmd(query string, ctx context.Context) tea.Cmd {
	index := m.searchIndex
	accounts := append([]config.AccountConfig(nil), m.accounts...)
	clients := append([]*imap.Client(nil), m.clients...)
	folders := m.searchFolders()
	aliases := m.folderAliases()
	return func() tea.Msg {
		var total, indexed, bodies, failedScopes, failedBodies int
		present := make(map[string]struct{})
		successfulScopes := make(map[string]struct{})
		for ai, account := range accounts {
			if ctx.Err() != nil {
				return universalSearchResultMsg{query: query, indexed: indexed, bodies: bodies, total: total, failedScopes: failedScopes, failedBodies: failedBodies, canceled: true}
			}
			if ai >= len(clients) || clients[ai] == nil {
				continue
			}
			cli := clients[ai]
			for _, folder := range folders {
				if ctx.Err() != nil {
					return universalSearchResultMsg{query: query, indexed: indexed, bodies: bodies, total: total, failedScopes: failedScopes, failedBodies: failedBodies, canceled: true}
				}
				scopeKey := account.Name + "\x00" + folder
				headers, err := cli.FetchHeaders(ctx, folder, 0)
				if err != nil {
					log.Printf("universal search scope failure: %v", err)
					failedScopes++
					continue
				}
				successfulScopes[scopeKey] = struct{}{}
				for _, e := range headers {
					e.Account = account.Name
					present[fulltext.Key(e)] = struct{}{}
					total++
					contactText := m.contactNamesFor(e.From, e.To, e.CC, e.BCC)
					index.UpsertHeader(e, contactText)
					indexed++
					if !index.NeedsBody(e) {
						continue
					}
					plain, html, _, _, _, _, fetchErr := cli.FetchBody(ctx, folder, e.UID)
					if fetchErr != nil {
						log.Printf("universal search body failure: %v", fetchErr)
						failedBodies++
						continue
					}
					index.UpsertBody(e, contactText, plain, html)
					bodies++
				}
			}
		}
		index.RemoveMissing(present, successfulScopes)
		bodies = index.Stats().Bodies
		matches := index.Search(query)
		fq := parseFilterQuery(query, time.Now(), aliases)
		filtered := matches[:0]
		for _, e := range matches {
			if fq.matchesEmail(e) {
				filtered = append(filtered, e)
			}
		}
		return universalSearchResultMsg{emails: filtered, query: query, indexed: indexed, bodies: bodies, total: total, failedScopes: failedScopes, failedBodies: failedBodies}
	}
}

func (m *Model) handleUniversalSearchResult(msg universalSearchResultMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m.universalSearchActive = false
	m.universalSearchRunning = false
	m.searchCancel = nil
	if msg.canceled {
		m.universalSearchResults = false
		m.status = "Search canceled; index remains partial."
		return m, nil
	}
	m.universalSearchResults = true
	m.universalSearchText = msg.query
	m.filterText = msg.query
	m.offTabFolder = "Search"
	m.emails = msg.emails
	m.markedUIDs = make(map[uint32]bool)
	var state string
	if msg.failedScopes > 0 || msg.failedBodies > 0 {
		state = fmt.Sprintf(" · partial (%d folder failures, %d body failures)", msg.failedScopes, msg.failedBodies)
	}
	m.status = fmt.Sprintf("Universal search: %d result(s), %d bodies indexed across %d envelopes%s · attachments not searched", len(msg.emails), msg.bodies, msg.total, state)
	if len(msg.emails) == 0 && (msg.failedScopes > 0 || msg.failedBodies > 0) {
		m.status = fmt.Sprintf("No matches in partial index: %d bodies indexed across %d envelopes · %d folder failures, %d body failures", msg.bodies, msg.total, msg.failedScopes, msg.failedBodies)
	}
	return m, m.sortEmails()
}

// ── IMAP server-side search ──────────────────────────────────────────────
//
// Universal filter (/) searches every configured account/folder with the
// private decoded-text index below.
//
// IMAP search (space + /) queries ALL emails across ALL folders on the server
// using IMAP SEARCH. Results are displayed in a temporary "Search" tab
// (like Spam or Drafts).
//
// Query syntax:
//   plain text   — matches FROM, SUBJECT, or TO (any field)
//   from:value   — matches FROM header only
//   subject:val  — matches SUBJECT header only
//   to:value     — matches TO header only

// imapSearchResultMsg carries results from a server-side IMAP search.
type imapSearchResultMsg struct {
	emails []imap.Email
	query  string
	err    error
}

// imapSearchAllCmd runs IMAP SEARCH across all configured folders.
// The query is expanded with addresses of known contacts whose display name
// matches (see expandSearchQueries) so searching "louise" also finds messages
// that only carry her bare address in the headers.
func (m Model) imapSearchAllCmd(query string) tea.Cmd {
	cli := m.imapCli()
	f := m.cfg.Folders
	folders := []string{
		f.Inbox, f.Sent, f.Trash, f.Drafts,
		f.ToScreen, f.Feed, f.PaperTrail, f.ScreenedOut,
		f.Archive, f.Waiting, f.Scheduled, f.Someday, f.Spam,
	}
	if f.Work != "" {
		folders = append(folders, f.Work)
	}
	queries := expandSearchQueries(query, m.contacts)
	return func() tea.Msg {
		var all []imap.Email
		seen := make(map[string]bool)
		var firstErr error
		for i, q := range queries {
			emails, err := cli.SearchAllFolders(nil, folders, q)
			if err != nil && i == 0 {
				firstErr = err // only the user's literal query reports errors
			}
			for _, e := range emails {
				key := fmt.Sprintf("%s\x00%d", e.Folder, e.UID)
				if !seen[key] {
					seen[key] = true
					all = append(all, e)
				}
			}
		}
		return imapSearchResultMsg{emails: all, query: query, err: firstErr}
	}
}

// expandSearchQueries returns the original query plus per-address queries for
// known contacts whose display name matches it. IMAP SEARCH can only match
// header text, and messages neomd sent carry bare addresses — without this a
// name search finds nothing in Sent. subject: queries are never expanded.
func expandSearchQueries(query string, cs *contacts.Store) []string {
	queries := []string{query}
	q := strings.TrimSpace(query)
	prefix := ""
	switch lower := strings.ToLower(q); {
	case strings.HasPrefix(lower, "subject:"):
		return queries
	case strings.HasPrefix(lower, "from:"):
		prefix, q = "from:", strings.TrimSpace(q[5:])
	case strings.HasPrefix(lower, "to:"):
		prefix, q = "to:", strings.TrimSpace(q[3:])
	}
	for _, addr := range cs.AddrsMatchingName(q, 3) {
		queries = append(queries, prefix+addr)
	}
	return queries
}

// updateIMAPSearch handles key input while the IMAP search prompt is active.
// Returns true if the key was consumed.
func (m *Model) updateIMAPSearch(key string) (tea.Model, tea.Cmd, bool) {
	if !m.imapSearchActive {
		return m, nil, false
	}

	switch key {
	case "esc":
		m.imapSearchActive = false
		m.imapSearchText = ""
		return m, nil, true

	case "enter":
		query := strings.TrimSpace(m.imapSearchText)
		if query == "" {
			m.imapSearchActive = false
			return m, nil, true
		}
		m.imapSearchActive = false
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, m.imapSearchAllCmd(query)), true

	case "backspace", "ctrl+h":
		runes := []rune(m.imapSearchText)
		if len(runes) > 0 {
			m.imapSearchText = string(runes[:len(runes)-1])
		}
		return m, nil, true

	default:
		if len(key) == 1 {
			m.imapSearchText += key
			return m, nil, true
		}
	}
	return m, nil, true
}

// handleIMAPSearchResult processes the result of an IMAP SEARCH command.
// Displays results in a temporary "Search" off-tab.
func (m *Model) handleIMAPSearchResult(msg imapSearchResultMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.err != nil {
		m.status = "Search error: " + msg.err.Error()
		m.isError = true
		return m, nil
	}
	if len(msg.emails) == 0 {
		m.status = fmt.Sprintf("No results for %q.", msg.query)
		return m, nil
	}
	m.imapSearchResults = true
	m.offTabFolder = "Search"
	m.emails = msg.emails
	m.markedUIDs = make(map[uint32]bool)
	m.filterActive = false
	m.filterText = ""
	m.status = fmt.Sprintf("Found %d email(s) for %q — esc to return, enter to open", len(msg.emails), msg.query)
	return m, m.sortEmails()
}

// everythingResultMsg carries results from fetching latest across all folders.
type everythingResultMsg struct {
	emails []imap.Email
	err    error
}

// fetchEverythingCmd fetches the latest N emails across all folders.
func (m Model) fetchEverythingCmd() tea.Cmd {
	cli := m.imapCli()
	f := m.cfg.Folders
	folders := []string{
		f.Inbox, f.Sent, f.Trash, f.Drafts,
		f.ToScreen, f.Feed, f.PaperTrail, f.ScreenedOut,
		f.Archive, f.Waiting, f.Scheduled, f.Someday, f.Spam,
	}
	if f.Work != "" {
		folders = append(folders, f.Work)
	}
	return func() tea.Msg {
		emails, err := cli.FetchLatestAllFolders(nil, folders, 50)
		return everythingResultMsg{emails: emails, err: err}
	}
}

// handleEverythingResult displays the "Everything" view.
func (m *Model) handleEverythingResult(msg everythingResultMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m.imapSearchText = ""
	if msg.err != nil {
		m.status = "Everything: " + msg.err.Error()
		m.isError = true
		return m, nil
	}
	if len(msg.emails) == 0 {
		m.status = "No emails found."
		return m, nil
	}
	m.offTabFolder = "Everything"
	m.emails = msg.emails
	m.markedUIDs = make(map[uint32]bool)
	m.filterActive = false
	m.filterText = ""
	m.status = fmt.Sprintf("Everything — %d most recent emails across all folders. esc to close.", len(msg.emails))
	return m, m.sortEmails()
}

// conversationResultMsg carries results from a conversation/thread fetch.
type conversationResultMsg struct {
	emails []imap.Email
	err    error
}

// fetchConversationCmd fetches all emails related to the given email's
// conversation across key folders (Inbox, Sent, Archive, etc.).
func (m Model) fetchConversationCmd(e *imap.Email) tea.Cmd {
	cli := m.imapCli()
	f := m.cfg.Folders
	// Search folders likely to contain conversation parts.
	folders := []string{f.Inbox, f.Sent, f.Archive, f.Waiting, f.Someday, f.Scheduled}
	if f.Work != "" {
		folders = append(folders, f.Work)
	}
	// Add current folder if not already included.
	cur := e.Folder
	found := false
	for _, fo := range folders {
		if fo == cur {
			found = true
			break
		}
	}
	if !found && cur != "" {
		folders = append(folders, cur)
	}

	// Normalize subject and collect participants.
	subject := normalizeSubject(e.Subject)
	participants := make(map[string]bool)
	for _, addr := range imap.SplitAddrs(e.From) {
		participants[addr] = true
	}
	for _, addr := range imap.SplitAddrs(e.To) {
		participants[addr] = true
	}
	for _, addr := range imap.SplitAddrs(e.CC) {
		participants[addr] = true
	}

	return func() tea.Msg {
		emails, err := cli.FetchConversation(nil, folders, subject, participants)
		return conversationResultMsg{emails: emails, err: err}
	}
}

// handleConversationResult displays the conversation/thread view.
func (m *Model) handleConversationResult(msg conversationResultMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m.imapSearchResults = false
	if msg.err != nil {
		m.status = "Thread: " + msg.err.Error()
		m.isError = true
		return m, nil
	}
	if len(msg.emails) == 0 {
		m.status = "No related emails found."
		return m, nil
	}
	m.offTabFolder = "Thread"
	m.emails = msg.emails
	m.markedUIDs = make(map[uint32]bool)
	m.filterActive = false
	m.filterText = ""
	m.status = fmt.Sprintf("Thread — %d email(s) in conversation. esc to close.", len(msg.emails))
	cmd := m.sortEmails()
	// Open the conversation on its first unread message (Superhuman-style:
	// the thread view lands you where attention is needed).
	items := m.inbox.Items()
	for i, it := range items {
		if item, ok := it.(emailItem); ok && !item.email.Seen {
			m.inbox.Select(i)
			break
		}
	}
	return m, cmd
}

// senderAddr returns the first bare address from an email's From header,
// or "" if the header is empty or unparseable.
func senderAddr(e *imap.Email) string {
	addrs := imap.SplitAddrs(e.From)
	if len(addrs) == 0 {
		return ""
	}
	return addrs[0]
}

// senderResultMsg carries results from a per-sender search across folders.
type senderResultMsg struct {
	addr   string
	emails []imap.Email
	err    error
}

// fetchSenderCmd searches all folders for every email from the given
// email's sender address.
func (m Model) fetchSenderCmd(e *imap.Email) tea.Cmd {
	addr := senderAddr(e)
	cli := m.imapCli()
	f := m.cfg.Folders
	folders := []string{
		f.Inbox, f.Sent, f.Trash, f.Drafts,
		f.ToScreen, f.Feed, f.PaperTrail, f.ScreenedOut,
		f.Archive, f.Waiting, f.Scheduled, f.Someday, f.Spam,
	}
	if f.Work != "" {
		folders = append(folders, f.Work)
	}
	return func() tea.Msg {
		if addr == "" {
			return senderResultMsg{addr: addr}
		}
		emails, err := cli.SearchAllFolders(nil, folders, "from:"+addr)
		return senderResultMsg{addr: addr, emails: emails, err: err}
	}
}

// handleSenderResult displays the "Sender" view — every email from one address.
func (m *Model) handleSenderResult(msg senderResultMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.err != nil {
		m.status = "Sender: " + msg.err.Error()
		m.isError = true
		return m, nil
	}
	if len(msg.emails) == 0 {
		m.status = fmt.Sprintf("No emails found from %q.", msg.addr)
		return m, nil
	}
	m.offTabFolder = "Sender"
	m.emails = msg.emails
	m.markedUIDs = make(map[uint32]bool)
	m.filterActive = false
	m.filterText = ""
	m.status = fmt.Sprintf("Sender — %d email(s) from %s across all folders. esc to close.", len(msg.emails), msg.addr)
	return m, m.sortEmails()
}

// viewIMAPSearchBar renders the search prompt at the bottom of the inbox.
func (m Model) viewIMAPSearchBar() string {
	cursor := ""
	if m.imapSearchActive {
		cursor = "█"
	}
	if m.imapSearchResults && !m.imapSearchActive {
		return styleHelp.Render(fmt.Sprintf("  search: %q — esc to close · from: subject: to: prefixes supported", m.imapSearchText))
	}
	return styleHelp.Render(fmt.Sprintf("  search (all folders): %s%s  · enter search · esc cancel · e.g. newsletter  from:simon  subject:invoice  to:team@", m.imapSearchText, cursor))
}
