# Vision

`neomd` exists so that one person can run their entire email life from the terminal, at the speed of a keystroke, with an inbox that contains only the senders they chose.
It removes the standing pains of email: an inbox flooded by strangers, writing trapped in rich-text web apps, filing busy-work that never ends, and tracking pixels that report every open.
It also removes the small waits that add up: actions that stall on a reload cycle, history that stops at the first page, and searches that answer nothing.
It began because its author wanted Neomutt's keyboard-driven speed combined with the HEY screener's sender control, GTD's process-once discipline, and Markdown as the native language of both reading and writing email.
The explicit bar is the best commercial keyboard-first client, named by its captain as the gold standard of that class: the same instant, shortcut-driven, zero-friction experience delivered from a terminal.
That intent is the whole product in one line: keyboard-first TUI email - write in Neovim, render as Markdown, screen senders first, organize emails once.

## The inbox is a chosen place

Ordinary clients treat the inbox as a place where everyone may arrive, then bury the user in filters, rules, and scoring algorithms that guess at what matters.
neomd inverts this: an unknown sender waits in ToScreen until the user decides, and one classification of a sender holds forever - screened-in, screened-out, Feed, or Papertrail.
The user decides who reaches the inbox, never an algorithm; this same gate doubles as phishing defense, because an impostor claiming to be a known sender lands in ToScreen where it looks immediately suspicious.
Decisions are made once and stored as plain text lists, so the screening brain is inspectable, editable, and syncable with no neomd in the loop.
After screening, GTD finishes the job: every email in the inbox is processed exactly once into next, Waiting, Someday, Scheduled, or Archive.
Filing hierarchies are rejected on purpose; emails fade into Archive, and search - over contacts and headers, on the server - finds them when needed.
Newsletters and receipts get their own quiet rooms in Feed and Papertrail, so deciding whether to read mail and whether to read news are separate acts.

## Built for one pair of hands

neomd serves the terminal person: someone who lives in Neovim and vim motions, writes in Markdown, and wants email to behave like the rest of their tooling instead of a browser tab.
It runs the captain's real mail: a work account and personal accounts side by side behind one OAuth setup, because one person's life spans several inboxes and switching between them is one keystroke, not a logout.
It is written by one such person for their own daily use, and shared publicly so others of the same shape can run, read, fork, and shape it; the fork exists so the workflow can be updated and optimized without asking anyone's permission, and users who need different folders fork the Go source.
On its author's machine it has taken over as the primary daily client, with the older Neomutt setup preserved unchanged purely as a fallback.
It is not for teams, shared inboxes, or collaboration; it is not for users who want a graphical client, a filing hierarchy, or an inbox that any sender can enter.
It is experimental software that moves, deletes, and flags mail directly on the IMAP server across all devices, and it says so up front: back up important email, try a secondary account first.

## What neomd owns

It owns the end-to-end terminal email experience: screening, reading, threading, composing in the user's own editor, sending, drafts, send-later, reminders, emoji reactions, iCalendar RSVPs, out-of-office replies, and multiple accounts behind one config with instant switching.
It owns the bytes that go on the wire: RFC-compliant MIME with Markdown as plain text plus rendered HTML, correct threading headers, deliverability across providers, and recipient-visible fidelity held to a hardening suite of byte-exact round-trip tests.
It owns a headless daemon that runs the background of the same workflow on an always-on machine: auto-screening, scheduled delivery, due reminders, and screened-in-only vacation replies that never reveal absence to strangers.
It owns a small set of hand-offs to tools the user already has: Neovim for composing, yazi for attachments, any LLM CLI for an AI draft pass, the local calendar app for invites, the browser for full-fidelity viewing, and a status-bar CLI for a panel widget.
It owns privacy defaults: spy pixels detected and blocked with attribution, credentials kept in config files, environment variables, or the OS keyring, TLS enforced with unencrypted ports refused, and secrets never printed in errors.

## What it refuses to own

It refuses to be a mail store: IMAP is the database, there is no local sync daemon or shadow copy to drift, and neomd stays consistent with the phone or webmail using the same account.
It refuses to grow folders: the folder set is fixed in code to the screener-plus-GTD workflow, because a small closed surface is what keeps processing predictable.
It refuses to compose in HTML: Markdown is the source of truth and HTML is derived output, never the other way around.
It refuses an in-app AI: intelligence arrives by handing the draft to whichever CLI the user configures, so neomd depends on no model, vendor, or subscription.
It refuses to score content for spamminess; screening is by sender and by the user's explicit word.
It refuses to be a service: no server of its own, no accounts, no telemetry, one Go binary and one config file.

## The experience

The experience is speed: every navigation, open, and move is a keystroke away and measured in milliseconds, not seconds, which is why provider latency is benchmarked, documented, and honest about which providers feel instant.
It is instant feedback: acting on a message updates the row at once instead of waiting for the next reload, and a failed action hands back the exact selection it started with.
It is continuity: the list keeps revealing older mail as the user scrolls, and a search returns everything that matches, because a feature that silently stops is a defect.
It is the confidence of processing email once: classify a sender one time, decide an email one time, and neither decision ever has to be made again.
It is the ease of looking away: drafts are backed up for recovery, sends require a review screen, discards ask for confirmation, moves can be undone, overdue scheduled mail shouts a warning instead of silently dying, and the hardening suite stands guard over every byte a recipient will see.
It is the calm of a quiet inbox: no unread strangers, no unread newsletters, no tracking, and desktop notifications only for senders the user named as VIP.

## Principles

Speed is the product; when a feature costs latency on every interaction, it must buy something the workflow cannot live without.
Fidelity to the wire outranks convenience; a mangled recipient, subject, attachment, or leaked Bcc reaches real people, so outgoing bytes are pinned by tests and never regressed for expediency.
Plain text wins ties; the compose buffer, screener lists, config, and drafts are all human-readable text that survives editors, sync, and time.
Nothing happens without a visible decision and a way back: review before send, confirm before discard, undo after move, recover after loss.
Outbound mail originates only in an explicit human act - a sent message, a scheduled send, a configured vacation reply - so no automated helper can ever speak in the user's voice.
Defects are fixed at the cause: a warning or a broken key is eliminated, never suppressed, hidden, or worked around.
Directness over layers: talk to the IMAP and SMTP servers directly, integrate by handing off to existing tools, and add no intermediate service, index, or daemon that can disagree with the server.
Security and privacy are defaults, not options: encrypted connections only, credentials protected, trackers blocked, and absence never disclosed to unscreened senders.
The single user's real workflow is the specification; features enter when the author's daily email demands them and are hardened as if business depended on it, because it does.

## Non-goals

- Custom or user-defined folders beyond the fixed screener and GTD set.
- A local mail store, index, or sync daemon beside IMAP.
- Rich-text or HTML composing; Markdown in, HTML out.
- Team, shared-inbox, or collaboration features.
- Making slow IMAP providers fast; latency is the provider's, and neomd documents it instead.
- A mobile-first client; running on Android is a working extra, not a target.
- Content-based spam scoring or any inbox ordering the user did not choose.

## A year from now

A folder switch on a good provider still feels instant, and the benchmark documentation still tells the truth about providers that cannot keep up.
Business email still arrives byte-exact, and the hardening suite has grown alongside every new field and path so that what is parse-back-asserted cannot silently break.
A change is not finished when its tests pass: it counts when it is proven in the live client on the owner's real machine, merged, deployed, and re-verified against the real inbox path.
The daemon quietly runs the background on a home server: screening arrives before the inbox is opened, scheduled mail goes out on time, reminders return when due, and vacation replies answer once and only the chosen.
The distance to the keyboard-first standard keeps shrinking in order of daily pain, instant actions and the small conveniences first, so the terminal client loses nothing to its graphical model.
Installing is still cloning the repository and running make install, upgrading is a version bump away, and a new user has classified their first sender within minutes of first launch.
The inbox still contains only senders the user chose, and the terminal still is the only place the user needs to answer email.
