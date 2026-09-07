package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/imap"
)

// ── Optimistic list updates ──────────────────────────────────────────────
//
// Archive / move / screen / remind all take an IMAP round-trip. Waiting for it
// (and then re-fetching the whole folder, as the pre-optimistic code did) made
// every keystroke feel like a page load. Instead the rows leave the list the
// instant the key is pressed, the IMAP work runs in the background, and the
// rows come back — visibly, with an error status — only if the server refuses.
//
// Batches are tracked by id so two actions fired back to back can each roll
// back independently; the id rides along on the batchDoneMsg / reminderDoneMsg
// that acknowledges it.

// emailKey identifies an email across folders (UIDs are only unique per folder).
func emailKey(folder string, uid uint32) string {
	return fmt.Sprintf("%s\x00%d", folder, uid)
}

// optimisticAct hides targets from the list immediately, then runs build() to
// get the IMAP command, tagging its completion message with this batch's id.
// The returned command performs the list refresh and the IMAP work together.
func (m *Model) optimisticAct(targets []imap.Email, label string, build func() tea.Cmd) tea.Cmd {
	if len(targets) == 0 {
		return nil
	}
	if m.cfg != nil && m.cfg.ReadOnly {
		m.status = readOnlyError(label).Error()
		m.isError = true
		return readOnlyBatchCmd(label)
	}
	m.optimisticSeq++
	id := m.optimisticSeq

	gone := make(map[string]bool, len(targets))
	for _, e := range targets {
		gone[emailKey(e.Folder, e.UID)] = true
	}
	kept := make([]imap.Email, 0, len(m.emails))
	var removed []imap.Email
	for _, e := range m.emails {
		if gone[emailKey(e.Folder, e.UID)] {
			removed = append(removed, e)
			continue
		}
		kept = append(kept, e)
	}
	m.emails = kept
	if m.optimistic == nil {
		m.optimistic = make(map[int][]imap.Email)
	}
	m.optimistic[id] = removed
	m.markedUIDs = make(map[uint32]bool)

	if len(targets) > 1 {
		m.status = fmt.Sprintf("%s %d…", label, len(targets))
	} else {
		m.status = label + "…"
	}
	m.isError = false
	return tea.Batch(m.applyFilter(), tagOptimistic(id, build()))
}

// tagOptimistic stamps a completion message with the batch id that produced it
// so the handler can retire (or roll back) the right batch.
func tagOptimistic(id int, c tea.Cmd) tea.Cmd {
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		switch msg := c().(type) {
		case batchDoneMsg:
			msg.optID = id
			return msg
		case reminderDoneMsg:
			msg.optID = id
			return msg
		default:
			return msg
		}
	}
}

// retireOptimistic drops a batch's rollback snapshot after the server confirms.
func (m *Model) retireOptimistic(id int) {
	delete(m.optimistic, id)
}

// rollbackOptimistic puts a failed batch's emails back in the list, so the user
// sees the rows return rather than silently losing the action.
func (m *Model) rollbackOptimistic(id int) tea.Cmd {
	restored, ok := m.optimistic[id]
	if !ok || len(restored) == 0 {
		return nil
	}
	delete(m.optimistic, id)
	m.emails = append(m.emails, restored...)
	return m.sortEmails()
}

// ── Infinite scroll ──────────────────────────────────────────────────────

// loadMoreThreshold is how close to the last row the cursor must come before
// the next page is fetched, so the append lands before the user hits the end.
const loadMoreThreshold = 5

// oldestUID returns the lowest UID held for folder, i.e. the paging cursor.
// Zero means we hold nothing for that folder and cannot page.
func oldestUID(emails []imap.Email, folder string) uint32 {
	var min uint32
	for _, e := range emails {
		if e.Folder != folder {
			continue
		}
		if min == 0 || e.UID < min {
			min = e.UID
		}
	}
	return min
}

// appendNewEmails appends only emails not already held, returning how many
// were added. Zero means the folder is exhausted.
func appendNewEmails(dst []imap.Email, extra []imap.Email) ([]imap.Email, int) {
	have := make(map[string]bool, len(dst))
	for _, e := range dst {
		have[emailKey(e.Folder, e.UID)] = true
	}
	added := 0
	for _, e := range extra {
		k := emailKey(e.Folder, e.UID)
		if have[k] {
			continue
		}
		have[k] = true
		dst = append(dst, e)
		added++
	}
	return dst, added
}

// maybeLoadMoreCmd fetches the next page when the cursor nears the bottom of
// the list. Ad-hoc views (search results, conversation, Everything) span
// folders and are not paged.
func (m *Model) maybeLoadMoreCmd() tea.Cmd {
	if m.loadingMore || m.moreExhausted || m.loading {
		return nil
	}
	if m.offTabFolder != "" || m.imapSearchResults || m.filterText != "" {
		return nil
	}
	if m.cfg.UI.InboxCount <= 0 {
		return nil // unlimited page size: the first fetch already has everything
	}
	n := len(m.inbox.Items())
	if n == 0 || m.inbox.Index() < n-loadMoreThreshold {
		return nil
	}
	folder := m.activeFolder()
	cursor := oldestUID(m.emails, folder)
	if cursor == 0 {
		return nil
	}
	m.loadingMore = true
	return m.fetchMorePageCmd(folder, cursor)
}

// fetchMorePageCmd fetches one more page of headers older than cursor.
func (m Model) fetchMorePageCmd(folder string, cursor uint32) tea.Cmd {
	n := m.cfg.UI.InboxCount
	return func() tea.Msg {
		cli := m.imapCli()
		if cli == nil {
			return moreEmailsLoadedMsg{folder: folder}
		}
		emails, err := cli.FetchHeadersBefore(nil, folder, cursor, n)
		return moreEmailsLoadedMsg{emails: emails, folder: folder, err: err}
	}
}
