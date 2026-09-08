# Superhuman-inspired keyboard map

This is the product map for neomd's vim-shaped keyboard. `internal/ui/keys.go`
drives the in-app `?` overlay and `docs/content/docs/keybindings.md`; this table
records the decision made for every shortcut on Superhuman v7's sheet. `h` opens
a centered Remind Me picker with quick picks and a natural-language field (for
example, `in 3 days`, `friday 2pm`, or `next week`).

| Superhuman key | neomd key | Status | Reason |
|---|---|---|---|
| ⌘K mark done | `e` | vim-adapted | Captain chose lowercase e for archive / done. |
| E mark not done | `E` | vim-adapted | Shift remains the opposite action and returns mail to Inbox. |
| / search | `/` | same | Vim filter is already the local-list search. Field tokens (from:, to:, subject:, has:attachment, before:/after:, in:) combine with free text. |
| Z remind me | `h` | vim-adapted | Captain reserved h for remind; the picker accepts quick picks and natural language. |
| ? shortcuts | `?` | same | Opens the searchable help overlay. |
| S star | dropped | dropped | neomd has no star state. |
| X select conversation | `x` | vim-adapted | x toggles the current selection; V extends it visually. |
| neomd sender view | `@` | same | Preserves cross-folder sender search while V is visual-select. |
| neomd quick peek | `<space><space>` | neomd-native | Leader+space toggles a preview pane (subject, headers, body) without leaving the inbox list; esc closes it and the cursor stays put. |
| neomd thread collapse | `<space>t` | neomd-native | Collapses threaded conversations to a single row (×N badge); enter/o opens the full conversation; `<space>t` again expands. |
| neomd screen-out action | `O` in the inbox | vim-adapted | O expands conversations in reader context; inbox keeps its screener action. |
| U read or unread | `n` | vim-adapted | n is the established read/unread action. |
| # trash | `dd` / `#` | vim-adapted | Vim delete operator plus the Superhuman alias. |
| ! spam | `!` / `$` | vim-adapted | ! is the Superhuman key; $ remains the screener alias. |
| shift-M mute | dropped | dropped | No mute mailbox or server operation exists. |
| ⌘U unsubscribe | dropped | dropped | Unsubscribe is a sender/server policy operation, not a mail move. |
| C compose | `s` / `c` | vim-adapted | Captain chose s; c remains a compatibility alias. |
| G then O reply all | `ctrl+r` | vim-adapted | `go` is reserved for the required Other/ScreenedOut folder. |
| R reply | `r` | same | Lowercase action follows the captain's vim convention. |
| F forward | `f` | same | Lowercase action follows the captain's vim convention. |
| ⌘O open links and attachments | `<space>o` | vim-adapted | Leader sequence avoids command-key chords. |
| tab cycle links and dates | `tab` | same | Field/link cycling remains terminal-native where available. |
| O expand message | `o` / `enter` | vim-adapted | Opening the selected mail is the existing reader transition. |
| shift-O expand all | `O` | same | Retained for conversation/browser expansion context. |
| L add/remove label | `v` / `gl` | vim-adapted | v opens the move picker and gl reaches it through goto. |
| l add/remove label | `l` | same | In the inbox l opens the move/label picker. |
| Y remove label | `y` | vim-adapted | Moves the selected mail back to Inbox. |
| shift-Y remove all labels | dropped | dropped | A mailbox move cannot represent arbitrary label removal. |
| V move | `v` | vim-adapted | v opens the same folder/label picker. |
| shift-U unread filter | `U` / `z` | same | Both the requested filter and the existing zoom alias work. |
| shift-S starred filter | dropped | dropped | No star state exists. |
| shift-I important filter | `I` / `<space>i` | vim-adapted | `I` approves the sender; `<space>i` toggles the focus view showing only screened-in (important) senders. |
| shift-R no-reply filter | dropped | dropped | No reliable no-reply classification exists. |
| G then I Inbox | `gi` | same | Required folder goto. |
| G then O Other | `go` | same | Maps Other to ScreenedOut. |
| G then S Starred | `gs` | vim-adapted | Reports that a distinct Starred folder is unavailable. |
| G then D Drafts | `gd` | same | Required folder goto. |
| G then T Sent | `gt` | vim-adapted | neomd's Sent folder takes the Superhuman goto. |
| G then E Done | `ge` | vim-adapted | Done maps to Archive. |
| G then H Reminders | `gh` | vim-adapted | Reminders map to Waiting. |
| G then M Muted | `gm` | vim-adapted | Someday is the closest configured destination. |
| G then ; Snippets | `g;` | same | Opens the live snippet picker. |
| G then ! Spam | `g!` | same | Required folder goto. |
| G then # Trash | `g#` | same | Required folder goto. |
| G then A All Mail | `ga` | vim-adapted | Maps to neomd's Everything view. |
| G then L label | `gl` | vim-adapted | Opens the folder/label picker. |
| G then 0 Open Day | dropped | dropped | neomd has no calendar view. |
| 0 then 0 Open Week | dropped | dropped | neomd has no calendar view. |
| T today | dropped | dropped | neomd has no calendar view; t remains thread view. |
| - previous day/week | dropped | dropped | neomd has no calendar view. |
| = next day/week | dropped | dropped | neomd has no calendar view. |
| B create event | dropped | dropped | neomd has no event editor. |
| → ← ↑ ↓ focus | j/k and arrows | vim-adapted | Vim movement is primary; arrows remain list-native aliases. |
| tab next split | tab | vim-adapted | Folder/tab cycling is the useful terminal equivalent. |
| shift-tab previous split | shift+tab | vim-adapted | Folder/tab cycling is the useful terminal equivalent. |
| enter open conversation | `enter` | same | Opens the selected message. |
| J/K next conversation | `j` / `k` | vim-adapted | Vim movement owns vertical navigation. |
| N/P next/previous message | `N` / `<space>p` | vim-adapted | In list view N jumps to the next thread containing unread mail and `<space>p` to the previous one (lowercase n stays mark-read; lowercase p stays dead). |
| space scroll down | `ctrl+d` | vim-adapted | Vim half-page movement is more consistent. |
| shift-space scroll up | `ctrl+u` | vim-adapted | Vim half-page movement is more consistent. |
| ⌘↑ / ⌘↓ top/bottom | `gg` / `G` | vim-adapted | Standard vim jumps. |
| ⌘N new window | dropped | dropped | Terminal client has one window. |
| ⌘T new tab | dropped | dropped | Terminal client uses folder tabs, not OS tabs. |
| ⌘shift-] / ⌘shift-[ tabs | `tab` / `shift+tab` | vim-adapted | Re-homed to folder tabs. |
| ⌘1-9 tabs | `<space>1…9` | vim-adapted | Leader avoids terminal account/control collisions. |
| ctrl-1-9 accounts | dropped | dropped | Account switching is not safely portable across terminals. |
| ⌘W close tab | `q` / `esc` | vim-adapted | Vim-style back closes the current view. |
| ⌘= increase font | dropped | dropped | Terminal theme/font is controlled by the terminal. |
| ⌘- decrease font | dropped | dropped | Terminal theme/font is controlled by the terminal. |
| ⌘0 reset font | dropped | dropped | Terminal theme/font is controlled by the terminal. |
| ctrl-/ copy page link | dropped | dropped | No browser page-link concept in the TUI. |
| copy message link | `y` | neomd-native | In the reader, opens a copy menu: `m` copies `neomd://mid/<url-encoded-message-id>` and `w` copies an available web-version link. Resolve the Message-ID link from a script with `neomd read <link>`. |
| ⌘B bold | dropped | dropped | Formatting belongs to the external editor. |
| ⌘I italic | dropped | dropped | Formatting belongs to the external editor. |
| ⌘U underline | dropped | dropped | Formatting belongs to the external editor. |
| ⌘K hyperlink | dropped | dropped | Formatting belongs to the external editor. |
| ⌘O color | dropped | dropped | Formatting belongs to the external editor. |
| ⌘shift-X strikethrough | dropped | dropped | Formatting belongs to the external editor. |
| ⌘shift-7 numbers | dropped | dropped | Formatting belongs to the external editor. |
| ⌘shift-8 bullets | dropped | dropped | Formatting belongs to the external editor. |
| ⌘shift-9 quote | dropped | dropped | Formatting belongs to the external editor. |
| ⌘↑ / ⌘↓ quote | dropped | dropped | Formatting belongs to the external editor. |
| tab indent list | dropped | dropped | Formatting belongs to the external editor. |
| shift-tab outdent list | dropped | dropped | Formatting belongs to the external editor. |
| ⌘] / ⌘shift-[ indent | dropped | dropped | Formatting belongs to the external editor. |
| ⌘shift-O To | dropped | dropped | Compose fields are directly visible in neomd. |
| ⌘shift-C Cc | `ctrl+b` | vim-adapted | One toggle exposes Cc and Bcc. |
| ⌘shift-B Bcc | `ctrl+b` | vim-adapted | One toggle exposes Cc and Bcc. |
| ⌘shift-F From | `ctrl+f` | vim-adapted | Cycles configured From identities. |
| ⌘shift-S subject | subject field | vim-adapted | Subject is edited in the compose form. |
| ⌘shift-M message | `e` in pre-send | vim-adapted | Reopens the external editor. |
| ⌘shift-A attach | `a` / `<space>a` | vim-adapted | File picker is re-homed to plain/leader keys. |
| ⌘shift-, discard | `x` / `esc` | vim-adapted | Confirmation protects unsent mail. |
| ⌘shift-I contacts to Bcc | dropped | dropped | Contact picker is separate and does not silently alter Bcc. |
| ⌘shift-P pop draft | dropped | dropped | Terminal client has no pop-out window. |
| ⌘shift-H remind | `<space>h` | vim-adapted | Leader sequence avoids a compose-field collision. |
| ⌘shift-L send later | `<space>l` / `l` | vim-adapted | Pre-send already has a direct l action. |
| `;` inline snippet | `<space>;` | vim-adapted | Leader form is explicit in compose; inserts at the field cursor, or into the message body. |
| ⌘; snippet picker | `;` / `g;` | vim-adapted | Captain's semicolon binding is global; m inside the picker opens the manager (create/edit/delete). |
| neomd command palette | `:` | neomd-native | Fuzzy-matched commands with a live preview line (`:done`, `:remind <time>`, `:move <folder>`, `:snip [name]`). |
| :smile emoji | `ctrl+e` reaction picker | vim-adapted | Structured emoji picker avoids editor syntax assumptions. |
| ⌘enter send | `ctrl+enter` / `enter` | vim-adapted | Terminals vary; enter remains the reliable fallback. |
| ⌘shift-enter send and done | `ctrl+enter` then `e` | dropped | Send-and-archive is explicit and safer as two actions. |
| ⌘shift-Z instant send | dropped | dropped | neomd always shows a pre-send review. |
| ⌘/ pop-out compose | dropped | dropped | Terminal client has no pop-out window. |
| ⌘D switch draft | `E` in Drafts | vim-adapted | Draft continuation is a reader action. |

## Dropped block

All remaining Superhuman command/option chords are intentionally unbound:
account switching chords, windows/pop-outs, browser tabs, focus arrows, font
size, formatting, colors, calendar navigation/events, mute/unsubscribe, star
and no-reply filters, and any shortcut that would require a command key or alt
key. They either have no neomd operation or belong to the external editor or
terminal itself.
