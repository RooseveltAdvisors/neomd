---
title: Omarchy Bar Plugin
weight: 2
---

Peek at your mail from the [Omarchy](https://omarchy.org) status bar — without
an unread counter staring at you all day.

**Plugin repository: [omarchy-neomd-plugin](https://github.com/sspaeti/omarchy-neomd-plugin)**

The [omarchy-neomd-plugin](https://github.com/sspaeti/omarchy-neomd-plugin) puts
a mail icon in the Omarchy bar that is deliberately **calm**: no unread badge, no
color change, ever. Mail only exists when you click the icon (or hit a hotkey) —
then a popup shows your HEY-style tabs (Inbox / ToScreen / Feed / PaperTrail)
with an in-panel reader, all powered by neomd's headless CLI subcommands.

## Features

- **Static icon** — nothing on the bar ever signals "new mail". The panel data
  is polled in the background so opening is instant, but the pill stays silent.
- **HEY-style tabs** — the same folders as neomd's TUI, switched with `H`/`L`,
  `1`–`4`, or a click. Unread counts appear on the tabs *inside* the panel only.
- **In-panel reader** — `l`/`Enter` opens the selected mail's Markdown body for
  a quick glance; `j`/`k` scroll, `h`/`Esc` go back, links open in the browser.
- **Strictly read-only** — bodies are fetched with `BODY.PEEK`, so glancing at
  a mail never sets `\Seen`, and nothing can be moved or classified from the
  panel. No inconsistencies with a running TUI.
- **Jump to neomd** — `o` (or right-click the icon) launches your real client,
  e.g. a tmux session running neomd.

## Requirements

The plugin drives neomd's headless one-shot subcommands (2026-08-25 or newer),
which exist exactly for integrations like this:

```sh
# Read-only header dump of folders as one JSON object
neomd list --folders Inbox,ToScreen,Feed,PaperTrail --limit 15

# One message body as JSON — BODY.PEEK, never marks the mail read
neomd read --folder Feed --uid 37112 [--max-bytes 65536]

# Classify a sender like the TUI's I/O/F/P keys (not used by the plugin,
# which stays read-only — but available for your own scripts)
neomd screen --from jane@example.com --action in|out|feed|paper
```

All three print a single JSON object and exit `0` even on failure
(`{"ok":false,"error":"..."}`), so a widget always has something to parse.
They use your normal `~/.config/neomd/config.toml` (accounts, folders,
keyring) — no second IMAP setup.

## Install

```sh
omarchy plugin add https://github.com/sspaeti/omarchy-neomd-plugin.git --enable
omarchy bar move io.github.sspaeti.neomd --section right
```

Optional hotkey in `~/.config/hypr/bindings.lua`:

```lua
o.bind("SUPER + CTRL + ALT + M", "neomd Mail panel",
  "omarchy-shell shell toggle io.github.sspaeti.neomd")
```

See the [plugin README](https://github.com/sspaeti/omarchy-neomd-plugin) for
all settings (icon, folders, default tab, poll cadence, jump command). Widget
settings are inline keys next to `id` in `~/.config/omarchy/shell.json` — no
nested `"settings"` object.
