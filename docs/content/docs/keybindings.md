---
title: Keybindings
weight: 2
---

Press `?` inside neomd to open the interactive help overlay. Start typing to filter shortcuts.

{{< callout type="info" >}}
The tables below are generated from [`internal/ui/keys.go`](https://github.com/ssp-data/neomd/blob/main/internal/ui/keys.go).
To update both the help overlay and this document at once, edit that file and run `make docs`.
{{< /callout >}}

<!-- keybindings-start -->

### Actions

| Key | Action |
|-----|--------|
| `e` | mark done / archive (alias A) |
| `E` | mark not done / move back to Inbox |
| `h` | remind me (quick picks / natural language) |
| `u` | undo the last action (move, delete, read state) |
| `U / z` | toggle unread-only view |
| `n` | mark read / unread |
| `N` | jump to the next thread with unread |
| `<space>p` | jump to the previous thread with unread |
| `<space>i` | toggle the focus view (important / screened-in senders) |
| `<space>t` | collapse threads to one row; enter opens the conversation |
| `I` | approve sender / screened-in |
| `o` | open the selected email |
| `O` | screen out in the inbox; expand all in a conversation |
| `P` | mark as PaperTrail |
| `B` | move to Work |
| `ctrl+e` | open the emoji reaction picker |
| `@` | show every email from the selected sender across folders |
| `! / $` | mark spam |
| `# / dd` | move to Trash |
| `F` | mark as Feed (uppercase: f is forward) |
| `<space><space>` | toggle the quick peek preview of the highlighted email |
| `S` | dry-run screen |
| `R` | reload the current folder |


### Conversations

| Key | Action |
|-----|--------|
| `r` | reply |
| `ctrl+r` | reply all |
| `f` | forward |
| `t` | open the full conversation thread (alias T) |
| `O` | expand all messages in a conversation / open in browser elsewhere |
| `enter` | open the selected email |
| `q / esc` | back to the inbox |
| `n / p` | next / previous message in a thread |


### Messages

| Key | Action |
|-----|--------|
| `s / c` | start a new email |
| `;` | open the snippets picker (m opens the snippet manager) |
| `o` | open the selected email (enter is the full-word alias) |
| `<space>o` | open links and attachments in the browser |
| `1-9` | open attachment 1-9 |
| `<space>1-0` | open link 1-10 |
| `<space>e` | open the raw email in the editor |
| `y` | open the copy menu (m Message-ID link; w web link) |


### Folders

| Key | Action |
|-----|--------|
| `gi` | Inbox |
| `gs` | Starred (when configured; otherwise reports unavailable) |
| `gd` | Drafts |
| `gt` | Sent |
| `ge` | Done / Archive |
| `gh` | Reminders / Waiting |
| `g;` | Snippets |
| `g!` | Spam |
| `g#` | Trash |
| `ga` | All Mail / Everything |
| `gl` | open the label / folder picker |
| `l` | add or remove a label with the folder picker |
| `go` | Other / ScreenedOut |
| `gm` | Muted / Someday |
| `gf` | Feed (neomd) |
| `gp` | PaperTrail (neomd) |
| `gk` | ToScreen (neomd) |
| `gw` | Waiting alias (neomd) |
| `gc` | Scheduled (neomd) |
| `gb` | Work (neomd, if configured) |
| `v / M*` | move to a folder with the picker / quick destination |
| `y` | remove label / move back to Inbox |


### Selection

| Key | Action |
|-----|--------|
| `x / m` | select or unselect the current email |
| `V` | visual-select mode; j/k extend the selection |
| `ctrl+a` | select all loaded emails |
| `esc` | clear visual selection |
| `ctrl+d / ctrl+u` | half-page down / up |


### Navigation

| Key | Action |
|-----|--------|
| `j / k` | move down / up |
| `gg / G` | jump to top / bottom |
| `/` | filter the loaded email list (from: to: subject: has:attachment before:/after: in:) |
| `N / <space>p` | next / previous thread with unread |
| `tab / shift+tab` | next / previous folder tab |
| `<space>1 … <space>9` | jump to a folder tab |
| `<space>/` | search all mail on the server |
| `?` | toggle this help overlay |


### Compose

| Key | Action |
|-----|--------|
| `a` | attach in pre-send |
| `<space>a` | attach in compose / pre-send |
| `<space>l / l` | send later |
| `<space>h` | remind while composing |
| `<space>;` | insert a snippet at the cursor / into the body |
| `<space>o` | open links / attachments |
| `ctrl+enter / enter` | send from pre-send |
| `d` | save a draft from pre-send |
| `e` | re-open the editor from pre-send |
| `ctrl+b` | toggle Cc/Bcc |
| `ctrl+f` | cycle From identities |
| `x / esc` | discard the unsent message (with confirmation) |


### Command line

| Key | Action |
|-----|--------|
| `:` | open the fuzzy command palette (type to match, live preview) |
| `:done` | archive (mark done) the selected emails |
| `:remind <time>` | remind the selected emails at a natural-language time |
| `:move <folder>` | move the selected emails to a folder |
| `:snip [name]` | open the snippet manager, or compose from a named snippet |
| `:screen / :s` | dry-run screening |
| `:screen-all / :sa` | screen all Inbox mail |
| `:reload / :r` | reload the current folder |
| `:search / :se` | search all mail |
| `:everything / :ev` | show All Mail |
| `:check / :ch` | show sender classification |
| `:reset-toscreen / :rts` | move ToScreen mail back to Inbox |
| `:delete-all / :da` | permanently delete the current folder |
| `:empty-trash / :et` | empty Trash |
| `:go-spam / :spam` | open Spam |
| `:create-folders / :cf` | create configured folders |
| `:debug / :dbg` | write a diagnostic report |
| `:quit / :q` | quit neomd |

<!-- keybindings-end -->
