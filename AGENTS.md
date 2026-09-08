# AGENTS.md — Feature Invariants

This file lists the key **user-visible behaviors that must not break**. It exists because
subtle regressions have slipped in before (e.g. the `·` reply indicator silently broke when
reply detection was refactored — see CHANGELOG 2026-07-09).

**How to use this file (for AI agents and humans):**

1. **Before changing code**, scan the section(s) that touch your area and keep those
   invariants intact.
2. **After changing code**, re-scan the same section(s) and confirm each invariant still
   holds — run the pinning test if one is listed (`go test ./internal/... -run TestName`).
3. **When adding a feature**, add its invariant here (one bullet: behavior, code anchor,
   pinning test).
4. **At the end of every user-visible change, add a `CHANGELOG.md` entry** (dated heading,
   bold title, what/why/where, test name).

Build commands, architecture, and API quirks live in `CLAUDE.md`. Feature docs live in
`README.md` and `docs/content/docs/`. This file is only the "do not break" list.

---

## Hardening Suite — run after ANY change to sending or IMAP

neomd is used for business email: a mangled recipient, subject, attachment name, or
leaked Bcc reaches real clients. The hardening suite is the safety net that catches
this class of regression. **After any change touching `internal/smtp`, `internal/imap`,
`internal/schedule`, `internal/contacts`, or the send path in `internal/ui`, run:**

```sh
go test ./... -run Hardening          # unit: full build→wire→parse-back, no network
make test-integration                 # live: real SMTP+IMAP fidelity (demo account)
```

What it pins (all byte-exact, not substring checks):

- **`internal/imap/roundtrip_hardening_test.go`** — every message the builders produce
  is parsed back with go-message (what the recipient's client does) *and* `parseBody`
  (what neomd itself does): From/To/Cc/Subject decode exactly, plain part before HTML,
  body lines survive quoted-printable (umlauts), attachment names AND bytes identical
  (0–255 binary fixture), Message-ID uses sender domain, threading headers only on
  replies (never duplicated), drafts keep Bcc + literal markdown + attachments,
  send-later delivers byte-identical messages with no `X-Neomd-*` leak — except
  the Date header, which the daemon stamps with the ACTUAL delivery time via
  `schedule.RewriteDate` (changing any other byte fails the test). Also:
  long multi-encoded-word subjects and emoji decode back exactly
  (`TestHardening_RoundTrip_SubjectExtremes`), 20+ recipients keep order and count
  (`_ManyRecipients`), body edge cases survive — two-space hard breaks, lone `.`
  and `--` lines, header-lookalike lines, 2000-char lines, CRLF input, no wire
  line ends in literal whitespace (`_BodyEdgeCases`), inline image + file
  attachment combined shape with byte-exact payloads both views
  (`_InlineImagePlusAttachment`), wire format — strict CRLF, ≤998-char lines,
  parseable Date, unique Message-IDs (`TestHardening_WireFormat`), and emoji
  reactions parse back with exact To/threading/body (`_ReactionMessage`).
- **`TestHardening_HeaderInjection`** — CRLF in subject/recipients/threading IDs and
  hostile attachment filenames (`"`/newline) can never smuggle headers
  (`sanitizeHeaderValue`, `sanitizeFilenameParam` in `internal/smtp/sender.go`;
  control-char rejection in `contacts.Add`).
- **`internal/ui/workflow_hardening_test.go`** — WORKFLOW-level: drives the real
  bubbletea Update handlers end-to-end (reply-all → real `neomd-*.md` compose
  file → editorDoneMsg → pre-send → enter) and delivers to a fake in-process
  TLS SMTP server, asserting only what the outside world sees (auth user,
  MAIL FROM, RCPT TO, delivered wire bytes). This catches composition/wiring
  bugs that per-function tests can't (e.g. swapped cc/bcc arguments in
  `sendEmailCmd` = Bcc leak — verified by mutation test).
  `TestHardening_Workflow_ReplyAllUsesReceivingAccount`: reply goes out via the
  account whose address received the email (auth + MAIL FROM + From header),
  reply-all Cc excludes every own address (logins, account Froms, sender
  aliases), auto_bcc reaches RCPT but never headers, threading headers point at
  the original, `·`-indicator data survives.
  `TestHardening_Workflow_MarkdownFileToWire`: a real markdown compose file
  (`# [neomd: ...]` headers, `[attach]`, `[html-signature]`, signature block)
  arrives with To/Cc/Bcc routing, umlaut subject, literal-markdown plain part,
  rendered HTML (bold/link/callout), text sig in both parts, HTML sig in HTML
  only, attachment bytes identical, and no internal marker delivered.
- **`internal/ui/send_hardening_test.go`** — RCPT TO is complete (To+Cc+Bcc), deduped,
  bare addresses only, and unchanged by contact-name decoration; messy input
  (double/trailing commas, whitespace) never yields empty or malformed RCPT
  entries (`TestHardening_RcptNoEmptyOrMalformedEntries`); auto_bcc merges
  case-insensitively against decorated forms and reaches RCPT exactly once
  (`TestHardening_AutoBccPipeline`).
- **`internal/integration_hardening_test.go`** (live) — draft attachment round-trip
  under its original filename with identical bytes (the 2026-08 rename incident),
  full send fidelity through a real server (umlaut subject + binary attachment,
  no Bcc/X-Neomd header on the delivered message), scheduled-queue APPEND/FETCH
  round-trip, reply threading through the server (delivered In-Reply-To/References
  match the original's real Message-ID, envelope view included —
  `TestIntegration_Hardening_ReplyThreadingThroughServer`), and body fidelity as
  the recipient decodes it (hard breaks, dot-stuffed `.` line, 1200-char line,
  umlauts/emoji — `TestIntegration_Hardening_BodyFidelityThroughServer`).

When you add a new field or path to outgoing messages, extend the round-trip suite in
the same commit — a field that isn't parse-back-asserted is a field that can silently
break.

## Authentication & Safety Modes

- **External OAuth2 helper** — `[[accounts]].oauth2_token_command` is a fixed argv
  token source (`internal/oauth2`, wired in `cmd/neomd/main.go`): only a leading
  `~/` in argv[0] expands, stderr is discarded, empty stdout fails, and the token
  remains in memory; it bypasses native OAuth client/URL/keyring/token-file setup.
  Test: `TestConfigureIMAPAuthUsesCommandTokenWithoutNativeOAuth`.
- **Read-only profile** — root `read_only` makes IMAP selection use EXAMINE, blocks
  every mutating IMAP method and UI outbound/action path before network access,
  suppresses first-run folder creation, blocks the mutating `screen` CLI, and
  refuses `--headless` (`internal/imap/client.go`, `internal/ui/model.go`,
  `cmd/neomd/main.go`). Tests: `TestReadOnlyBlocksMutationsBeforeDial`,
  `TestReadOnlyBlocksPR5ActionsBeforeNetwork`.

**Hardening assertions may only be extended, never weakened.** If a hardening test
fails after a code change, the default assumption is that the CODE broke a
user-visible contract — investigate the code first. Relaxing, deleting, or rewriting
a hardening assertion to make a change pass requires the user's explicit approval in
that conversation; "the test was too strict" is not a decision an agent makes alone.

---

## Per-email Reminders

- **Superhuman-style reminders** — `H` in the reader prompts for a future time,
  stores the selected message in the configured Reminders folder (the legacy
  Waiting config key remains supported) with `X-Neomd-Reminder-*` metadata,
  and moves the original to recoverable Trash without SMTP. The headless daemon
  and TUI background sync return due reminders to Inbox; metadata remains visible
  as the `⏰` indicator, and the Reminders list date shows the fire time. Tests:
  `TestReminderKeyStartsPerEmailPrompt`, `TestParseReminderSection`,
  `TestParseHeaderAndStatus`, `TestReminderDueTimeShownInListRow`.

## Reply & Threading

- **Agent-readable Message-ID links** — `neomd read <neomd://mid/...|Message-ID>`
  resolves the copied reader link across configured accounts/folders with
  read-only IMAP `SEARCH`/`BODY.PEEK`; `--json`, `--raw`, `--folder`, `--account`,
  and stdin batches are supported. Keep `cmd/neomd/read_agent.go`,
  `internal/imap/read.go`, and `internal/link/message_id.go` aligned with the
  read-only/no-flag-mutation contract. Tests: `TestSearchMessageIDsAndReadRawMessage`,
  `TestRunAgentReadUsesURIHostFolderAndStdinBatch`.

- **Message-ID share links** — in the reader, `y` opens the copy menu and `m` copies
  `neomd://mid/<url-encoded-message-id>`; the URI encodes the RFC Message-ID, never
  an IMAP UID or folder path. Tests: `internal/link/message_id_test.go`,
  `internal/ui/copy_menu_test.go`.

- **Every copy goes out as OSC 52 first** — `copyToClipboard`
  (`internal/ui/clipboard.go`) writes the escape sequence to the terminal, and only
  then tries a local tool (`wl-copy`/`xclip`/`xsel`/`pbcopy`); either succeeding is a
  success. neomd is routinely run over ssh, where a local tool sets the wrong
  machine's clipboard or fails outright, so a local-tool-only copy is a silent no-op
  for the user. A tool whose display variable is unset is skipped, never run and
  failed. Inside tmux the sequence is emitted twice — plain and DCS-passthrough
  wrapped with every ESC doubled — so it lands under either `set-clipboard on` or
  `allow-passthrough on`. Tests: `TestYankMenuMessageIDIsOSC52Encoded`,
  `TestOSC52TmuxPassthroughDoublesEscapes`,
  `TestLocalClipboardToolSkippedWithoutDisplay`.

- **`·` reply indicator** — after sending a reply, the original email gets the IMAP
  `\Answered` flag (`MarkAnswered` in `internal/imap/client.go`, called from `sendEmailCmd`
  in `internal/ui/model.go`) and the inbox shows `·` (or `·╰` inside a thread,
  `internal/ui/inbox.go`). The local flag also updates immediately on `sendDoneMsg` without
  a refetch. Tests: `TestSendDoneMsgUpdatesAnsweredFlag`, `TestReplyIndicatorWithThread`.
- **Reply tracking survives pre-send round-trips** — `pendingIsReply` is *session-scoped*:
  re-edit (`e`), spell check (`s`), AI handoff (`i`), and CC/BCC edit (`ctrl+b`) from
  pre-send must all preserve `replyToUID`/`replyToFolder` and the `In-Reply-To`/`References`
  headers. The flag is cleared only when the compose session ends (send, discard, editor
  abort/error/empty, new compose/forward). Test: `TestEditorDoneReplyTrackingSurvivesReEdit`.
- **Threading headers on every reply-ish send** — regular replies, emoji reactions
  (`ctrl+e`), and iCalendar RSVPs all set `In-Reply-To` + `References` so conversations
  thread in Gmail/Outlook/Apple Mail. Tests: `TestBuildReactionMessage_ThreadingHeaders`,
  `TestBuildRSVPMessage_ThreadingHeadersBracketed`.
- **Reply prefix handling** — `Re:` is prepended only when the subject doesn't already
  start with `re:`/`aw:`/`sv:`/`vs:` (case-insensitive); localized prefixes are treated as
  replies, never double-prefixed. Test: `TestHasReplyPrefix`.
- **Reply From auto-selection** — replying picks the From address matching the email's
  To/CC; in the Sent folder the user's own address is in `From` instead
  (`matchFromForReply`, `internal/ui/model.go`).
- **Reply-all excludes all own addresses** — both IMAP login addresses (`account.User`)
  and send-as addresses (accounts + `[[senders]]` aliases) are stripped from CC. Test:
  `TestReplyAllExcludesAllOwnAddresses`.
- **Threaded inbox rendering** — threads grouped via `In-Reply-To`/`Message-ID` with
  subject+participant fallback, `│`/`╰` connectors, newest on top; the Sent folder is
  intentionally **not** threaded. Tests: `TestNormalizeSubject`, `TestParticipantMatch`.
- **Sender view (`@`)** — from the inbox list, searches `from:<addr>` (bare address
  from the selected email) across every configured folder via the same
  `SearchAllFolders` IMAP infra as `/`-search, opening results in a `Sender` off-tab
  (`internal/ui/search.go`: `senderAddr`, `fetchSenderCmd`, `handleSenderResult`).
  `@` preserves sender search while `V` is now vim visual-select mode; `E`/`F` remain
  occupied by mark-not-done and Feed. Tests: `TestSenderAddr`, `TestHandleSenderResultSetsOffTabAndEmails`,
  `TestHandleSenderResultNoMatches`.

## Compose → Pre-send → Send Pipeline

- **Pre-send round-trip preservation** — every path that re-opens the editor or returns to
  pre-send (`e`, `s`, `i`, `ctrl+b`, draft continue, `:recover`) must preserve: body,
  attachments (re-injected as `# [attach]` lines — editor body is source of truth),
  Bcc, selected From, and reply tracking (see above). History: CHANGELOG 2026-04-08,
  2026-05-06, 2026-07-02, 2026-07-09 — this area regresses easily.
- **MIME structure by content** (`BuildMessage` in `internal/smtp/sender.go`):
  no attachments → `multipart/alternative`; file attachments → `mixed > alternative`;
  inline images → `related > (alternative + image parts with Content-ID)`; both →
  `mixed > (related > alt+images) + file parts`. Tests: `TestBuildMessage*`.
- **Inline images** — local `<img src="/abs/path">` rewritten to `cid:`; paths with spaces
  use the `![](<path>)` angle-bracket form and URL-decoding before file read; remote
  `https://` images (HTML signatures) are fetched (10 s timeout) and embedded as `cid:`,
  falling back to the URL on fetch failure. Tests: `TestBuildMessage_WithInlineImage`,
  `TestBuildMessage_InlineImagePathWithSpaces`.
- **`[attach]` markers are visible plain text** — `# [attach] /path` (header form) and
  `[attach] /path` (inline form), never HTML comments (treesitter hides them in nvim).
  Only regular files are accepted (`filterValidAttachments`); skipped paths are surfaced
  in the status bar. Test: `TestFilterValidAttachments`.
- **BCC privacy** — Bcc is excluded from message headers but included in SMTP `RCPT TO`;
  comma-separated recipients are split into individual RCPT commands; `auto_bcc` is
  deduped and visible (never silent). Test: `TestCollectRcptTo`.
- **RFC compliance** — Message-ID uses the sender's domain (never `@neomd`/`@localhost`);
  quoted-printable encodes trailing whitespace before CRLF (`=20`) so Markdown two-space
  hard breaks survive SMTP relays; text/plain part comes before text/html.
- **Signatures** — per-account `[accounts.signature_block]` overrides the global block
  all-or-nothing via `Config.Signature(account)`; text signature goes to editor + plain
  part, HTML signature to HTML part only; `[html-signature]` placeholder controls
  inclusion per-email and is extracted right before send. Test: `TestSignature`.
- **Drafts** — saved as plain text only (multipart caused round-trip corruption), keep
  `Bcc`; every compose session is backed up to `~/.cache/neomd/drafts/` (`:recover`);
  discarding unsent mail always asks y/n confirmation.
- **Draft attachments keep their original filename** — continuing a draft writes
  extracted attachments into a fresh temp dir under their real basename (never a
  mangled `CreateTemp` name — the sent filename is the path's basename). Duplicates
  dedupe as `name-2.ext`; traversal/empty names sanitized (`writeAttachmentsTemp`,
  `internal/ui/model.go`). Test: `TestWriteAttachmentsTempPreservesFilename`.
- **Contact-name decoration is headers-only** — bare To/Cc addresses with a harvested
  contact name become `Name <addr>` in message headers at send time, but
  `collectRcptTo` always uses the raw undecorated fields and Bcc is never decorated
  (BCC privacy + comma-split RCPT must not break). Unsafe names (`,<>"`), and any
  part already containing `<`, are left untouched; `first.last@` derivation never
  fires for role mailboxes (`contacts.Decorate`, `contacts.DeriveName`). Tests:
  `TestHarvestNameAndDecorate`, `TestAddRejectsUnsafeNames`, `TestDeriveName`.
- **Compose autocomplete matches contact names** — To/Cc/Bcc suggestions come from
  the contacts store (matched by display name OR address, suggested as
  `Name <addr>`) plus screener-list addresses (decorated when the name is known);
  nil store never panics. Typed `Name <addr>` recipients are harvested into the
  cache at send/schedule time (`harvestTypedRecipients`) so a name typed once
  persists. Tests: `TestComposeSuggestions_*`, `TestHarvestTypedRecipients`.
- **Send-later queue marker is display-only** — queued messages are identified by
  a peek'd `X-Neomd-Send-At` header-fields fetch (`Email.SendAt`,
  `parseSendAtSection`) and shown with a `[send-later …]` subject prefix at
  render time only; the stored subject/message is NEVER mutated (it is what gets
  delivered) and GTD mail sharing the Scheduled folder stays unmarked. Tests:
  `TestParseSendAtSection`, `TestSendLaterPrefix`, live assert in
  `TestIntegration_Hardening_ScheduledQueueRoundTrip`.
- **Re-saving a working copy replaces, never duplicates — and never deletes
  early** — continuing a queued send-later message OR a saved draft via `E`
  tracks the original (`requeue` in `internal/ui/model.go`); it is moved to
  Trash (recoverable) ONLY after the replacement is successfully scheduled,
  saved, or sent. Regular emails opened with `E` are NEVER tracked (a received
  mail must never be trashed by sending an edit of it). Abort/discard/error
  paths clear the tracking without touching the original; a failed cleanup
  warns loudly (double-delivery risk). Tests:
  `TestContinueDraftTracksQueuedOriginal`, `TestScheduleDoneReplacesQueuedOriginal`,
  `TestSendDoneReplacesQueuedOriginal`, `TestSaveDraftReplacesPreviousVersion`,
  `TestContinueRegularEmailNeverTracked`, `TestEditorAbortKeepsQueuedOriginal`,
  `TestRequeueCleanupFailureWarns`.
- **Send-later watchdog** — the TUI checks Scheduled on startup and every
  background sync; queued messages more than 10 min past due (`overdueGrace`)
  raise a red OVERDUE status warning — a down daemon must never silently
  swallow a scheduled email. Fetch errors are silent (retried next tick).
  Tests: `TestCountOverdueScheduled`, `TestOverdueScheduledWarns`.
- **Send later never double-delivers** — the daemon claims a due Scheduled message
  with `\Flagged` *before* SMTP; flagged leftovers are skipped and logged, never
  retried automatically (`processScheduled`, `internal/daemon/daemon.go`). The
  delivered message and its Sent copy must carry **no** `X-Neomd-*` headers
  (`X-Neomd-Rcpt` contains Bcc!); messages without `X-Neomd-Send-At` in the
  Scheduled folder (GTD items) are never touched. At delivery the Date header
  is rewritten to the actual send time (`schedule.RewriteDate` — only that one
  line may change), so recipients and the Sent copy show when the mail went
  out, not when it was queued. Tests:
  `TestInjectExtractRoundTrip`, `TestExtractIgnoresRegularMail`,
  `TestSMTPConfigFor`, `TestRewriteDate`.
- **Out-of-office replies are screened-in-only and reply exactly once** — the daemon's
  `processOOO` (`internal/daemon/daemon.go`, logic in `internal/ooo/`) answers only
  senders classified `CategoryInbox`, only mail dated after the persisted period start,
  and only to the sender address (Reply-To pref, From fallback) — **never** Cc, never
  our own addresses. The `~/.cache/neomd/ooo_replied` cache is written *before* SMTP
  (crash ≠ duplicate); changing `[ooo].from`/`until` resets it (`ooo.Period`); a set
  `from` arms OOO at that day's midnight or the exact `"YYYY-MM-DD HH:MM"` time,
  interpreted in `[ooo].timezone` (IANA; default daemon-machine local time)
  (`ooo.StartFor`/`parseWhen`/`location`; date-only `until` stays inclusive
  end-of-day; unknown timezone fails safe: inactive). `[ooo].accounts` switches the
  watched inboxes to those accounts (per-account From/signature/Sent, lazy extra IMAP
  clients via `oooClientFor`; unknown or imap_disabled names are hard errors; the
  reply-once cache stays shared across accounts). Tests: `TestResolveOOOAccounts`. Loop guard both directions:
  incoming `Auto-Submitted`/`Precedence: bulk|junk|list`/`List-Id`/`List-Unsubscribe`
  mail is skipped; outgoing replies carry `Auto-Submitted: auto-replied` +
  `X-Auto-Response-Suppress: All`. Replies are built with the same
  `BuildMessageWithThreading` pipeline as composed mail (MIME shape, signatures,
  threading) and copied to Sent; the reply subject is the configured `[ooo].subject`
  verbatim (default "Out of Office" — the original subject is never appended).
  An `ooo.toml` next to config.toml replaces the whole `[ooo]` block and is
  re-read by the daemon EVERY pass (hot reload — `make ooo` syncs it to the server with
  no restart); invalid TOML fails safe (no replies). TUI ignores `[ooo]` entirely.
  Tests: `TestShouldConsider`, `TestIsAutoGenerated`, `TestCache_*`, `TestBuildReply_*`,
  `TestActive_*`, `TestLoadOOOOverride_*`.
- **Callouts** — `> [!note]` / `> [!tip]` / `> [!warning]` (with or without space after
  `>`) render as styled boxes in the HTML part and as emoji text (no blockquote markers)
  in the plain part. Tests: `TestToHTML_Callout_*`, `TestFormatCalloutsForPlainText_*`.
- **Listmonk interception** — sending to a configured trigger address creates a scheduled
  Listmonk campaign instead of SMTP delivery; pre-send shows list IDs + template + delay.
  Tests: `TestResolveListIDs`, `TestResolveTemplateID`.
- **From cycling (`ctrl+f`)** — SMTP credentials, Sent-folder destination, and From header
  must all follow the selected identity (accounts first, then `[[senders]]` aliases).
  Tests: `TestPresendSMTPAccount`, `TestReactionAutoSelectsCorrectFromAndSMTP`,
  `TestSentDraftsIMAPClient_*`.

## Screener (HEY-style)

- **Priority order** — spam > screened_out > feed > papertrail > screened_in; per-address
  entries always beat `@domain` entries. Tests: `TestClassify`, `TestClassifyForScreen`.
- **Reclassification is atomic** — classifying removes the address from ALL conflicting
  lists (snapshot/rollback on failure, both files and moved emails). Test:
  `TestCrossListCleanup_Reclassification`.
- **Empty lists pause screening** — TUI and headless daemon both skip auto-screening until
  the first sender is classified (prevents sweeping a fresh inbox to ToScreen). Test:
  `TestScreenInbox_EmptyScreenerLists`.
- **Screener destinations may never be Trash** — refuses to run otherwise. Test:
  `TestValidateScreenerSafetyRejectsTrashDestination`.
- **ToScreen sender-level classify** — acting on one unmarked message applies to all
  queued mail from that sender.
- **Lists are line-based with `#` comments** (full-line and inline); daemon only reads
  lists and moves mail, never writes classifications.
- **External classify goes through `neomd screen`** (`cmd/neomd/screen.go`) — the CLI
  subcommand for widgets (omarchy bar plugin) must keep TUI parity: list update BEFORE
  any move, sender-level expansion over all queued ToScreen mail, and the
  `ValidateScreenerSafety` Trash gate. Its read-only siblings `neomd list`
  (`cmd/neomd/list.go`) and `neomd read` (`cmd/neomd/read.go`) must never mutate flags
  or folders — `read` goes through `FetchBody`'s `BODY.PEEK`, so a widget glance can
  never set `\Seen`. All three always emit one JSON object and exit 0 even on failure.
  Tests: `TestRunScreen_ApproveMovesAllFromSender`,
  `TestRunScreen_RefusesTrashDestination`, `TestRunList_JSONShape`,
  `TestRunRead_JSONShape`.

## Inbox Display

- **Rows never overflow the terminal width** — complex scripts (Bengali/Arabic/Thai/emoji)
  collapse to `·` for display only; CJK passes through (East Asian Wide is deterministic);
  the original subject is never mutated (reply/forward/thread logic uses the real RFC
  subject). Tests: `TestRowFitsTerminalWidth`, `TestDisplaySafe`.
- **Indicator columns** — unread, `·` replied, `°` spy pixel, `│`/`╰` thread connectors.
- **Undo (`u`) is universal** — the journal (`undoStack []undoAction`, `pushUndo` in
  `internal/ui/model.go`) records every reversible effect of the most recent action:
  IMAP moves (archive, screener I/O/F/P/$, dd/# trash, M-chords, move picker) AND
  `\Seen` toggles (`n`, ctrl+n mark-all-read). `u` pops LIFO and reverses the exact
  action type via `undoActionCmd`; server-side failure surfaces in the status line.
  Screener classification lists are intentionally kept on undo (mail moves back, sender
  lists don't). Permanent `X` delete is never journaled. Tests: `TestUndoJournal*`,
  `TestUndoNothingToDo`, `TestUndoRefusesInReadOnly`, integration
  `TestIntegration_IMAPMoveAndUndo`.
- **Quick peek (`<space><space>`)** — leader+space toggles a preview pane (From/To/
  Subject/body) of the highlighted email without leaving the inbox list; `esc` or the
  chord again closes it, the cursor never moves, j/k re-targets the pane, and the body
  is fetched with BODY.PEEK (never sets `\Seen`). The pane is exactly `peekPaneHeight`
  lines so the status bar never overflows. Tests: `TestPeek*` in
  `internal/ui/undo_peek_test.go`.

- **Search matches contact names** — `internal/contacts` harvests `Name <addr>` pairs
  from loaded headers into `~/.cache/neomd/contacts`; the local `/` filter appends
  resolved names to its haystack, and server-side search (`space /`) expands a name
  query into per-address queries (never for `subject:`), deduped by folder+UID.
  Envelope To/CC/BCC keep display names (`formatEnvelopeAddr`) — names that would
  break comma-splitting fall back to the bare address. Tests:
  `TestFormatEnvelopeAddr`, `TestExpandSearchQueries`,
  `TestContactNamesForResolvesBareAddresses`.

- **Universal `/` search** — `internal/search` uses Bleve's memory-only embedded index
  to index every configured account and
  folder using decoded envelope/body text fetched with `BODY.PEEK`; sender/recipient
  names and addresses, subject, body terms, prefixes, small typos, relevance, and
  explicit field/date/folder filters are supported. Missing folders/body fetches,
  cancellation, and indexing scope are visible; an incomplete index must not show a
  false zero. Search results retain their owning account so opening and actions use
  the correct IMAP client. Attachments are intentionally not retained or searched.
  Tests: `internal/search/index_test.go`,
  `TestUniversalSearchInputIsCancelable`,
  `TestUniversalSearchResultShowsPartialStateAndKeepsResultActionable`,
  `TestSearchResultUsesOwningAccountForMessageClient`.
- **The user's `[contacts]` file is read-only** — `contacts.MergeFile` only reads;
  neomd persists exclusively to its own cache (`config.ContactsCachePath()`), so the
  cache can be deleted anytime and rebuilds from harvesting + the file. The picker
  (`space c`, `internal/ui/contacts_picker.go`) copies via the shared
  `copyToClipboard` helper and never mutates the store. Tests: `TestMergeFileGoogleCSVRealExport`,
  `TestContactsPickerFilterAndSelect`.

## Reading & Security

- **Spy pixels blocked** — two layers: curated denylist with attribution
  (`internal/imap/tracker_list.go`) + generic 1×1 heuristic; glamour never fetches remote
  resources; results cached in `~/.cache/neomd/spy_pixels` (`+key` spy / `-key` clean).
  Tests: `TestSpyPixelDetection`, `TestSpyPixelSpacersNotFlagged`.
- **Browser view (`O`) injects CSP** — `script-src 'none'; frame-src 'none';
  object-src 'none'`; remote images intentionally allowed there. Test:
  `TestIntegration_BrowserSanitization`.
- **Link opening whitelist** — only `http://`, `https://`, `mailto:` schemes. Test:
  `TestURLSchemeValidation`.
- **Attachment open safety** — executable extensions are saved but never auto-opened;
  magic-byte mismatch detection (`http.DetectContentType`) blocks disguised files;
  sender-supplied filenames are sanitized against path traversal (`..`, separators) in
  every write path (downloads, `.ics`, `cid:` temp files).
- **Timer-based mark-as-read** — opening an email marks `\Seen` only after
  `mark_as_read_after_secs` (default 7 s); quick peeks stay unread; reply/forward marks
  immediately.

## IMAP & Runtime Resilience

- **Retry policy** — `withConnRetry` (one retry) only for read-only ops (FETCH/SEARCH/
  STATUS); mutating ops (MOVE/APPEND/STORE) use `withConn`, never retried (duplicate-mail
  risk). NOOP health probe after 2+ min idle handles suspend/resume.
- **`safeGo` everywhere** — background goroutines must use `safeGo()` (panic → 
  `~/.cache/neomd/crash.log`), never bare `go func()`. Maps passed to goroutines are
  snapshotted on the main goroutine first (spy-pixel cache race, CHANGELOG 2026-05-08).
- **Nothing blocks the bubbletea Update loop** — notifications have a 2 s timeout, no DNS
  lookups in the send path, background sync never tight-loops on error. Test:
  `TestSend_TimeoutCannotBlockTUI`.
- **`imap_disabled = true` accounts produce nil clients by design** — every helper that
  resolves an IMAP client must skip nil entries (send, Sent-copy, `\Answered`, `:debug`,
  headless). Tests: `internal/ui/imap_client_helpers_test.go`.

## Notifications & Theming

- **Desktop notifications are VIP-only and TUI-only** — fire solely for senders/domains in
  `notify.txt` (independent of screener categories); the headless daemon never notifies;
  first run records a UID baseline (state key uses the IMAP folder name, not the UI label)
  so enabling never floods; `[notifications].folders` allowlist matches via `LabelFor()`.
  Tests: `TestMaybeNotify_*`, `TestShouldNotify`.
- **Default theme never drifts** — `kanagawa` must stay byte-for-byte identical to the
  pre-theming palette; `[theme]` overrides merge on top of any built-in. Tests:
  `TestKanagawaDefault`, theme override/fallback tests.

## Config & Credentials

- **Config validation on load** — host:port format, port 1–65535, required fields;
  `$VAR`/`${VAR}` expansion in `user`/`password`. Tests: `TestValidate*`, `TestExpandEnv`.
- **`password = "keyring"` sentinel** — resolved in `config.Load()` so IMAP, SMTP, and
  `[[senders]]` aliases all see it; preserved with a warning if the keyring is unavailable.
  Test: `TestUseKeyring`.
- **Secrets never leak** — token files/dirs written with restrictive permissions; error
  messages never include tokens/passwords. Tests: `TestTokenErrors_NoTokenLeak`,
  `TestSaveToken_FilePermissions`.

## Core Keyboard Contract (e / h / s / ;)

The client is driven from the home row. These four keys are the product, not a convenience
layer — do not rebind them, and do not let a new binding shadow one.

- **`e` archive · `h` remind · `s` start an email · `;` snippets** — bound in both the
  inbox (`updateInbox`) and the reader (`updateReader`) in `internal/ui/model.go`. The
  pre-5.0 keys still work as aliases (`A` archive, `H` remind, `c` compose).
  Test: `TestEmailBindingMap`, `TestReaderArchiveAndRemindKeys`.
- **Mailbox ACTION keys are UPPERCASE.** `I` approve, `O` screen-out, `P` papertrail,
  `B` work, `F` feed (uppercase because `f` is forward), `$` spam; the former lowercase
  `i`/`p`/`b` no longer fire (pinned by `TestLowercaseMailboxActionsAreDead`). The four
  core keys stay lowercase by contract, as do `o` open, `t` thread, `n` read toggle.
  Vim-shaped selection uses `x/m`, `V`, and `ctrl+a`; `dd/#` trashes; `u` undoes; `U/z`
  filters unread; `ctrl+d/u` are half-page movement. The final map and dropped shortcuts
  live in `docs/keys.md`; `internal/ui/keys.go` drives the overlay and generated docs.
- **`h` no longer exits the reader** — `q`/`esc` do. Reverting that would shadow remind.
- **Reader `e` moved the old $EDITOR view to `<space>e`.**
- **Superhuman/vim shortcut map is user-visible contract** — `internal/ui/keys.go`,
  `docs/keys.md`, and the context footers must stay aligned: `x/m` select,
  `V` extends selection with j/k, `dd/#` trash, `u` undo, `U/z` unread-only,
  `ctrl+d/u` half-page, and the final `g` folder routes. Tests:
  `TestDocumentedKeyBindings`, `TestReadOnlyBlocksExpandedMutatingBindingsBeforeNetwork`.
- **Bindings must not fire inside a text field** — the inbox handler's early returns for
  `cmdMode`, `imapSearchActive`, `filterActive`, `reminderActive` and `pendingKey` are what
  guarantee this; new bindings go in the main `switch` *after* those guards, never before.
  Test: `TestEmailBindingsGuardedInsideInputFields`.

## Instant Actions (optimistic UI)

- **Every list action applies before the IMAP round-trip.** Archive/delete/screen/move/
  remind route through `Model.optimisticAct` (`internal/ui/optimistic.go`): the rows leave
  `m.emails` and the list immediately, the IMAP command runs behind it, and `batchDoneMsg`
  / `reminderDoneMsg` *confirm in place* — **no folder re-fetch**. Regressing to
  `fetchFolderCmd` on ack is the failure mode this exists to prevent.
  Test: `TestOptimisticArchiveIsInstant`.
- **Failures roll back visibly** — the rows return to the list and `isError` is set; a
  refused action is never silently dropped. Test:
  `TestOptimisticArchiveRollsBackVisiblyOnFailure`.
- **Overlapping actions are independent** — each batch gets an `optID`; one batch's ack must
  never consume another's rollback snapshot.
  Test: `TestOverlappingOptimisticBatchesRollBackIndependently`.

## Infinite Scroll

- **Reaching the bottom appends the next page** — `maybeLoadMoreCmd` fires within
  `loadMoreThreshold` rows of the end and pages on UID via
  `imap.Client.FetchHeadersBefore(folder, beforeUID, n)`.
  Test: `TestScrollToBottomTriggersNextPage`.
- **The cursor must not move when a page lands** — appends go through `sortEmails` and then
  re-`Select` the previous index. Test: `TestNextPageAppendsAndKeepsScrollPosition`.
- **Ad-hoc views are never paged** — IMAP search results, `Everything`, conversation and
  sender views span folders, so UID paging is meaningless there.
  Test: `TestScrollDoesNotPageAdHocViews`.
- **An empty page latches `moreExhausted`** so the client stops asking; a full folder load
  (`emailsLoadedMsg`) resets paging and clears pending optimistic snapshots.
  Tests: `TestEmptyNextPageMarksFolderExhausted`, `TestFullFolderLoadResetsPagingState`.

## Snippets

- **`;` reads `<config dir>/snippets/*.md` at open time** (`internal/snippets`), so a new
  template needs no restart. An optional leading `Subject:` line sets the subject; the rest
  is the body, staged through `mailtoBody` so `launchEditorCmd` drops it into the editor
  buffer. Tests: `TestSnippetParseSplitsSubjectAndBody`, `TestSnippetPickerComposesPrefilled`.

## Keybindings & Docs

- **`internal/ui/keys.go` is the single source of truth** — drives the `?` overlay and the
  generated `docs/keybindings.md` (`make docs`, runs in `make build`). Never hand-edit the
  markdown tables.
- **Avoid modifier keys for new bindings** — user's tmux prefix is `C-t`; compose text
  fields still own their editing chords, while inbox `ctrl+a`/`ctrl+d`/`ctrl+u` follow
  the final vim map. Prefer plain letters, especially on pre-send.
- **README.md syncs to the docs site** (`scripts/sync-readme-to-docs.sh` via `make docs`).

## Superhuman UX Pack

- **Fuzzy `:` palette with live preview** — prefix matches win exclusively; fuzzy
  subsequence matches only fill in when nothing matches by prefix (`matchCmdsFuzzy`,
  `internal/ui/palette.go`). `matchCmdLine` splits the first word (command) from the
  rest (argument); the cmdline shows a concrete preview via `neomdCmd.preview` before
  enter. New commands: `:done`, `:remind <time>`, `:move <folder>`, `:snip [name]`.
  Tests: `TestMatchCmdsFuzzyFallback`, `TestMatchCmdLineSplitsArgument`,
  `TestCmdLinePreviewDescribesEffect`, `TestCmdDoneArchivesViaOptimisticPath`.
- **`/` filter field tokens** — `parseFilterQuery` (`internal/ui/filter_parser.go`):
  `from: to: subject: has:attachment before:/after: in:` AND-combine with free text;
  quoted values may contain spaces. `parseFilterDate` accepts past ISO dates and
  yesterday/today/tomorrow/last-week/N-days-ago (when.Parse is future-only and must
  not be used for filter dates). Tests: `TestParseFilterQuery*`,
  `TestFilterQueryMatchesEmail`, `TestApplyFilterWithFieldTokens`,
  `TestFolderAliasesResolveLabelsAndAliases`.
- **Focus view `<space>i`, thread collapse `<space>t`, unread-thread jumps `N`/`<space>p`**
  — lowercase `i`/`p` stay dead (the inbox handler must not grow cases for them;
  `TestLowercaseMailboxActionsAreDead` still passes). Collapse folds only multi-row
  thread blocks (threadCount > 0) and enter/o on such a row opens the conversation;
  `expandedThreads` keys on `normalizeSubject`. `jumpUnreadThread` works on the
  displayed rows, skips the cursor's own block, and wraps.
  Tests: `TestFocusView*`, `TestCollapse*`, `TestJumpUnreadThread*`, `TestNKeyJumps*`.
- **Snippet insert mode + manager** — `<space>;` from compose/pre-send opens the picker
  in insert mode: single-line body → insert at the focused field's cursor; multi-line →
  `mailtoBody` (editor buffer); pre-send → appended to `pendingSend.body`. `:snip` (or
  `m` in the picker) opens the manager: n (name prompt → $EDITOR), e/enter edit,
  d + y/n delete. `Model.snippetDir` overrides the snippet directory for tests.
  Tests: `TestSnippetInsert*`, `TestSnippetManagerCreateEditDelete`.
- **Undo names what it reversed** — `undoAction.describe()` feeds `undoDoneMsg.desc`
  ("Undone: move 3 email(s) back to Archive"). Test: `TestUndoActionDescribe`.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
