// Package daemon provides a headless background mode for neomd that
// continuously screens emails without launching the TUI.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sspaeti/neomd/internal/config"
	"github.com/sspaeti/neomd/internal/imap"
	"github.com/sspaeti/neomd/internal/ooo"
	"github.com/sspaeti/neomd/internal/schedule"
	"github.com/sspaeti/neomd/internal/screener"
	"github.com/sspaeti/neomd/internal/smtp"
)

// Daemon runs headless email screening in the background.
type Daemon struct {
	cfg      config.Config
	imapCli  *imap.Client
	screener *screener.Screener
	logger   *slog.Logger

	// oooClients caches extra IMAP connections for [ooo].accounts that are
	// not the daemon's own login (lazily created, reused across passes).
	oooClients map[string]*imap.Client
}

// New creates a new daemon instance.
func New(cfg config.Config, imapCli *imap.Client, sc *screener.Screener) *Daemon {
	return &Daemon{
		cfg:      cfg,
		imapCli:  imapCli,
		screener: sc,
		logger:   slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
}

// Run starts the daemon and blocks until interrupted by a signal.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("neomd daemon starting", "version", "headless")

	// Check bg_sync_interval
	intervalMins := d.cfg.UI.BgSyncInterval
	if intervalMins <= 0 {
		return fmt.Errorf("bg_sync_interval must be > 0 for daemon mode (got %d)", intervalMins)
	}
	interval := time.Duration(intervalMins) * time.Minute
	d.logger.Info("screening interval configured", "minutes", intervalMins)

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Set up file watcher for screener list changes
	watcher, err := d.setupFileWatcher()
	if err != nil {
		return fmt.Errorf("setup file watcher: %w", err)
	}
	defer watcher.Close()

	// Create a context that gets cancelled on signal
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Run initial screening immediately (if screener lists are not empty)
	d.logger.Info("running initial screening")
	if err := d.screenInbox(ctx); err != nil {
		d.logger.Error("initial screening failed", "error", err)
	}
	if err := d.processScheduled(ctx); err != nil {
		d.logger.Error("send-later pass failed", "error", err)
	}
	if err := d.processOOO(ctx); err != nil {
		d.logger.Error("out-of-office pass failed", "error", err)
	}

	// Set up ticker for periodic screening
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	d.logger.Info("daemon running", "interval", interval.String())

	// Main event loop
	for {
		select {
		case <-ticker.C:
			d.logger.Info("running scheduled screening")
			if err := d.screenInbox(ctx); err != nil {
				d.logger.Error("screening failed", "error", err)
			}
			if err := d.processScheduled(ctx); err != nil {
				d.logger.Error("send-later pass failed", "error", err)
			}
			if err := d.processOOO(ctx); err != nil {
				d.logger.Error("out-of-office pass failed", "error", err)
			}

		case event := <-watcher.Events:
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				d.logger.Info("screener list changed, reloading", "file", filepath.Base(event.Name))
				if err := d.reloadScreener(); err != nil {
					d.logger.Error("failed to reload screener", "error", err)
				}
			}

		case err := <-watcher.Errors:
			d.logger.Error("file watcher error", "error", err)

		case sig := <-sigChan:
			d.logger.Info("received signal, shutting down", "signal", sig.String())
			cancel()
			return nil
		}
	}
}

// setupFileWatcher creates a file watcher for all screener list files.
func (d *Daemon) setupFileWatcher() (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Watch all screener list files
	paths := []string{
		d.cfg.Screener.ScreenedIn,
		d.cfg.Screener.ScreenedOut,
		d.cfg.Screener.Feed,
		d.cfg.Screener.PaperTrail,
		d.cfg.Screener.Spam,
	}

	watchedDirs := make(map[string]bool)
	for _, path := range paths {
		if path == "" {
			continue
		}
		// Watch the directory containing the file, since some editors
		// create temp files and rename them (which breaks file watches)
		dir := filepath.Dir(path)
		if !watchedDirs[dir] {
			if err := watcher.Add(dir); err != nil {
				d.logger.Warn("failed to watch directory", "dir", dir, "error", err)
			} else {
				d.logger.Info("watching directory for changes", "dir", dir)
				watchedDirs[dir] = true
			}
		}
	}

	return watcher, nil
}

// reloadScreener reloads the screener from disk.
func (d *Daemon) reloadScreener() error {
	newScreener, err := screener.New(screener.Config{
		ScreenedIn:  d.cfg.Screener.ScreenedIn,
		ScreenedOut: d.cfg.Screener.ScreenedOut,
		Feed:        d.cfg.Screener.Feed,
		PaperTrail:  d.cfg.Screener.PaperTrail,
		Spam:        d.cfg.Screener.Spam,
		Notify:      d.cfg.Screener.Notify,
	})
	if err != nil {
		return fmt.Errorf("reload screener: %w", err)
	}
	d.screener = newScreener
	d.logger.Info("screener reloaded successfully")
	return nil
}

// processScheduled delivers due send-later messages queued in the Scheduled
// folder (see internal/schedule). Claim protocol: a message is marked
// \Flagged right before SMTP delivery so a crash mid-send can never deliver
// twice — a message that is due but already flagged is skipped and logged
// (unflag it in the Scheduled folder to retry, or delete it to cancel).
func (d *Daemon) processScheduled(ctx context.Context) error {
	folder := d.cfg.Folders.Scheduled
	if folder == "" {
		return nil
	}
	emails, err := d.imapCli.FetchHeaders(ctx, folder, 0)
	if err != nil {
		return fmt.Errorf("fetch scheduled headers: %w", err)
	}
	now := time.Now()
	for _, e := range emails {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := d.imapCli.FetchRaw(ctx, folder, e.UID)
		if err != nil {
			d.logger.Error("send-later: fetch raw failed", "uid", e.UID, "error", err)
			continue
		}
		job, cleaned, found, err := schedule.Extract(raw)
		if err != nil {
			d.logger.Error("send-later: invalid queued message", "uid", e.UID, "subject", e.Subject, "error", err)
			continue
		}
		if !found {
			continue // regular mail in the Scheduled folder (GTD) — not ours
		}
		if job.SendAt.After(now) {
			d.logger.Info("send-later: not due yet", "uid", e.UID, "subject", e.Subject, "send_at", job.SendAt.Format(time.RFC3339))
			continue
		}
		if e.Flagged {
			d.logger.Warn("send-later: skipping claimed message — a previous delivery attempt did not finish; unflag it in Scheduled to retry, delete it to cancel", "uid", e.UID, "subject", e.Subject)
			continue
		}
		smtpCfg, err := d.smtpConfigFor(job.From)
		if err != nil {
			d.logger.Error("send-later: cannot resolve SMTP account", "from", job.From, "error", err)
			continue
		}
		// Claim before sending — duplicate-send protection.
		if err := d.imapCli.MarkFlagged(ctx, folder, e.UID); err != nil {
			d.logger.Error("send-later: claim (\\Flagged) failed, skipping", "uid", e.UID, "error", err)
			continue
		}
		// Stamp the ACTUAL delivery time — the Date set at build time is when
		// the user queued the message, which recipients (and neomd's Sent
		// view) would otherwise show as the send time.
		cleaned = schedule.RewriteDate(cleaned, time.Now())
		if err := smtp.SendRaw(smtpCfg, job.Rcpt, cleaned); err != nil {
			d.logger.Error("send-later: SMTP delivery failed — message stays flagged in Scheduled; unflag to retry", "uid", e.UID, "subject", e.Subject, "error", err)
			continue
		}
		if err := d.imapCli.SaveSent(ctx, d.cfg.Folders.Sent, cleaned); err != nil {
			d.logger.Error("send-later: delivered, but Sent copy failed", "uid", e.UID, "error", err)
		}
		if err := d.imapCli.ExpungeAll(ctx, folder, []uint32{e.UID}); err != nil {
			d.logger.Error("send-later: delivered, but delete from Scheduled failed — delete it manually to avoid confusion", "uid", e.UID, "error", err)
			continue
		}
		d.logger.Info("send-later: delivered", "subject", e.Subject, "rcpt", strings.Join(job.Rcpt, ", "), "scheduled_for", job.SendAt.Format(time.RFC3339))
	}
	return nil
}

// processOOO sends out-of-office auto-replies for the active OOO period
// ([ooo] config block). Safety invariants (see internal/ooo and AGENTS.md):
// only screened-in senders, only to the sender address (never Cc), once per
// sender per period (persisted cache, marked BEFORE sending so a crash can
// never duplicate), never to auto-generated mail, and replies carry
// Auto-Submitted/X-Auto-Response-Suppress loop-protection headers.
func (d *Daemon) processOOO(ctx context.Context) error {
	// Hot-reload ooo.toml every pass so `make ooo` (scp of that one file)
	// enables/edits/disables OOO without a daemon restart.
	oooCfg := d.cfg.OOO
	if override, err := config.LoadOOOOverride(d.cfg.OOOFile); err != nil {
		return err
	} else if override != nil {
		oooCfg = *override
	}
	if !ooo.Active(oooCfg, time.Now()) {
		return nil
	}
	period := ooo.Period(oooCfg)
	cache, err := ooo.LoadCache(config.OOOCachePath(), period)
	if err != nil {
		return fmt.Errorf("load ooo cache: %w", err)
	}
	if cache.Started().IsZero() {
		if err := cache.SetStarted(ooo.StartFor(oooCfg, time.Now())); err != nil {
			return fmt.Errorf("persist ooo start: %w", err)
		}
		d.logger.Info("ooo: activated — replying only to NEW mail from screened-in senders",
			"until", period, "cache", config.OOOCachePath())
	}

	// Which inboxes get auto-replies: the [ooo].accounts list, or the
	// daemon's own account when unset. Each account replies from its own
	// From address with its own signature.
	targets, err := resolveOOOAccounts(d.cfg, oooCfg.Accounts)
	if err != nil {
		return fmt.Errorf("ooo: %w", err)
	}
	if targets == nil {
		from, acct, err := d.oooIdentity()
		if err != nil {
			return fmt.Errorf("ooo: resolve From identity: %w", err)
		}
		acct.From = from
		targets = []config.AccountConfig{acct}
	}
	var firstErr error
	for _, acct := range targets {
		if err := d.processOOOAccount(ctx, oooCfg, cache, acct); err != nil {
			d.logger.Error("ooo: account pass failed", "account", acct.Name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// resolveOOOAccounts maps [ooo].accounts names to configured accounts
// (case-insensitive). nil names → nil (caller falls back to the daemon's own
// account). Unknown or imap_disabled accounts are errors — a typo must not
// silently skip an inbox.
func resolveOOOAccounts(cfg config.Config, names []string) ([]config.AccountConfig, error) {
	if len(names) == 0 {
		return nil, nil
	}
	accounts := cfg.ActiveAccounts()
	out := make([]config.AccountConfig, 0, len(names))
	for _, name := range names {
		found := false
		for _, a := range accounts {
			if strings.EqualFold(a.Name, name) {
				if a.IMAPDisabled {
					return nil, fmt.Errorf("ooo account %q has imap_disabled — cannot watch its inbox", name)
				}
				out = append(out, a)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown ooo account %q (check [ooo].accounts against [[accounts]] names)", name)
		}
	}
	return out, nil
}

// oooClientFor returns an IMAP client for the account: the daemon's own
// client when it is the same login, otherwise a lazily-created (and cached)
// extra connection.
func (d *Daemon) oooClientFor(acct config.AccountConfig) *imap.Client {
	if d.imapCli != nil && strings.EqualFold(acct.User, d.imapCli.User()) {
		return d.imapCli
	}
	if d.oooClients == nil {
		d.oooClients = make(map[string]*imap.Client)
	}
	if c, ok := d.oooClients[strings.ToLower(acct.User)]; ok {
		return c
	}
	host, port, _ := strings.Cut(acct.IMAP, ":")
	useTLS, useSTARTTLS := imap.InferSecurity(port, acct.STARTTLS)
	c := imap.New(imap.Config{
		Host:        host,
		Port:        port,
		User:        acct.User,
		Password:    acct.Password,
		TLS:         useTLS,
		STARTTLS:    useSTARTTLS,
		TLSCertFile: acct.TLSCertFile,
	})
	d.oooClients[strings.ToLower(acct.User)] = c
	return c
}

// processOOOAccount runs one OOO pass over a single account's inbox,
// replying from that account's own From address and signature.
func (d *Daemon) processOOOAccount(ctx context.Context, oooCfg config.OOOConfig, cache *ooo.Cache, acct config.AccountConfig) error {
	cli := d.oooClientFor(acct)
	from := acct.From
	sig := d.cfg.Signature(acct)
	body, err := ooo.Body(oooCfg, sig)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("empty reply body — set [ooo].body or body_file")
	}
	smtpCfg := d.smtpConfig(acct, from)

	emails, err := cli.FetchHeaders(ctx, d.cfg.Folders.Inbox, 0)
	if err != nil {
		return fmt.Errorf("fetch inbox headers: %w", err)
	}
	own := d.ownAddresses()
	screenedIn := func(a string) bool { return d.screener.Classify(a) == screener.CategoryInbox }

	for _, e := range emails {
		if err := ctx.Err(); err != nil {
			return err
		}
		addr, due := ooo.ShouldConsider(e, cache.Started(), own, screenedIn, cache.HasReplied)
		if !due {
			continue
		}
		raw, err := cli.FetchRaw(ctx, d.cfg.Folders.Inbox, e.UID)
		if err != nil {
			d.logger.Error("ooo: fetch raw failed, will retry next pass", "uid", e.UID, "from", e.From, "error", err)
			continue
		}
		if ooo.IsAutoGenerated(raw) {
			// Mark so we don't re-fetch this sender's mail every pass.
			if err := cache.MarkReplied(addr); err != nil {
				d.logger.Error("ooo: cache write failed", "error", err)
			}
			d.logger.Info("ooo: skipping auto-generated mail (loop protection)", "from", e.From, "subject", e.Subject)
			continue
		}
		reply, err := ooo.BuildReply(from, addr, oooCfg.Subject, body, sig.HTML, e)
		if err != nil {
			d.logger.Error("ooo: build reply failed", "to", addr, "error", err)
			continue
		}
		// Claim before sending — duplicate-reply protection (same idea as the
		// send-later \Flagged claim).
		if err := cache.MarkReplied(addr); err != nil {
			d.logger.Error("ooo: cache write failed, skipping send to avoid duplicates", "to", addr, "error", err)
			continue
		}
		if err := smtp.SendRaw(smtpCfg, []string{addr}, reply); err != nil {
			d.logger.Error("ooo: SMTP delivery failed — sender stays marked, no retry (remove the address from the ooo_replied cache to retry)",
				"to", addr, "error", err)
			continue
		}
		if err := cli.SaveSent(ctx, d.cfg.Folders.Sent, reply); err != nil {
			d.logger.Error("ooo: replied, but Sent copy failed", "to", addr, "error", err)
		}
		d.logger.Info("ooo: auto-reply sent", "account", acct.Name, "from", from, "to", addr, "in_reply_to", e.Subject)
	}
	return nil
}

// oooIdentity picks the From identity for OOO replies: default_from when set,
// otherwise the daemon's own IMAP account. The returned account is used for
// signature resolution.
func (d *Daemon) oooIdentity() (string, config.AccountConfig, error) {
	accounts := d.cfg.ActiveAccounts()
	// The daemon's IMAP account is the stable fallback identity.
	var daemonAcct *config.AccountConfig
	for i, a := range accounts {
		if d.imapCli != nil && strings.EqualFold(a.User, d.imapCli.User()) {
			daemonAcct = &accounts[i]
			break
		}
	}
	if daemonAcct == nil && len(accounts) > 0 {
		daemonAcct = &accounts[0]
	}
	if d.cfg.DefaultFrom != "" {
		fa := imap.SplitAddrs(d.cfg.DefaultFrom)
		if len(fa) > 0 {
			for _, a := range accounts {
				if aa := imap.SplitAddrs(a.From); len(aa) > 0 && strings.EqualFold(aa[0], fa[0]) {
					return a.From, a, nil
				}
			}
			for _, s := range d.cfg.Senders {
				if sa := imap.SplitAddrs(s.From); len(sa) > 0 && strings.EqualFold(sa[0], fa[0]) {
					acct := config.AccountConfig{}
					if daemonAcct != nil {
						acct = *daemonAcct
					}
					return s.From, acct, nil
				}
			}
		}
	}
	if daemonAcct == nil {
		return "", config.AccountConfig{}, fmt.Errorf("no account configured")
	}
	return daemonAcct.From, *daemonAcct, nil
}

// ownAddresses returns every configured From address (accounts and senders),
// lowercased and bare, so the daemon never OOO-replies to itself.
func (d *Daemon) ownAddresses() map[string]bool {
	own := make(map[string]bool)
	for _, a := range d.cfg.ActiveAccounts() {
		for _, addr := range imap.SplitAddrs(a.From) {
			own[strings.ToLower(addr)] = true
		}
		if a.User != "" && strings.Contains(a.User, "@") {
			own[strings.ToLower(a.User)] = true
		}
	}
	for _, s := range d.cfg.Senders {
		for _, addr := range imap.SplitAddrs(s.From) {
			own[strings.ToLower(addr)] = true
		}
	}
	return own
}

// smtpConfigFor resolves SMTP settings for a queued message's From header:
// accounts first, then [[senders]] aliases (via their account= reference,
// falling back to the first account like the TUI does).
func (d *Daemon) smtpConfigFor(from string) (smtp.Config, error) {
	addrs := imap.SplitAddrs(from)
	if len(addrs) == 0 {
		return smtp.Config{}, fmt.Errorf("unparseable From %q", from)
	}
	target := addrs[0]
	accounts := d.cfg.ActiveAccounts()
	for _, a := range accounts {
		if fa := imap.SplitAddrs(a.From); len(fa) > 0 && fa[0] == target {
			return d.smtpConfig(a, from), nil
		}
	}
	for _, s := range d.cfg.Senders {
		fa := imap.SplitAddrs(s.From)
		if len(fa) == 0 || fa[0] != target {
			continue
		}
		for _, a := range accounts {
			if strings.EqualFold(a.Name, s.Account) {
				return d.smtpConfig(a, from), nil
			}
		}
		if len(accounts) > 0 {
			return d.smtpConfig(accounts[0], from), nil
		}
	}
	return smtp.Config{}, fmt.Errorf("no account or [[senders]] alias matches %q", target)
}

func (d *Daemon) smtpConfig(a config.AccountConfig, from string) smtp.Config {
	host, port, _ := strings.Cut(a.SMTP, ":")
	cfg := smtp.Config{
		Host:        host,
		Port:        port,
		User:        a.User,
		Password:    a.Password,
		From:        from,
		STARTTLS:    a.STARTTLS,
		TLSCertFile: a.TLSCertFile,
	}
	// OAuth2 token source is only available for the daemon's own IMAP account.
	if d.imapCli != nil && strings.EqualFold(a.User, d.imapCli.User()) {
		cfg.TokenSource = d.imapCli.TokenSource()
	}
	return cfg
}

// screenInbox fetches inbox emails and screens them.
func (d *Daemon) screenInbox(ctx context.Context) error {
	// Skip screening if screener lists are empty (mirrors TUI behavior)
	// This prevents sweeping all unknown senders to ToScreen on first run
	if d.screener.IsEmpty() {
		d.logger.Info("screening paused: screener lists are empty (classify your first sender to activate)")
		return nil
	}

	inboxFolder := d.cfg.Folders.Inbox

	// Fetch inbox headers (0 means fetch all)
	emails, err := d.imapCli.FetchHeaders(ctx, inboxFolder, 0)
	if err != nil {
		return fmt.Errorf("fetch inbox headers: %w", err)
	}

	if len(emails) == 0 {
		d.logger.Info("inbox is empty, nothing to screen")
		return nil
	}

	d.logger.Info("fetched inbox emails", "count", len(emails))

	// Classify emails using shared screener logic
	moves, err := screener.ClassifyForScreen(d.screener, emails, d.cfg.Folders)
	if err != nil {
		return fmt.Errorf("classify emails: %w", err)
	}

	if len(moves) == 0 {
		d.logger.Info("no emails need screening")
		return nil
	}

	d.logger.Info("emails to screen", "count", len(moves))

	// Execute moves
	movedCount := 0
	for i, mv := range moves {
		uid := mv.Email.UID
		from := mv.Email.From
		subject := mv.Email.Subject
		dst := mv.Dst

		if err := ctx.Err(); err != nil {
			d.logger.Warn("screening interrupted", "moved", movedCount, "total", len(moves))
			return err
		}

		_, err := d.imapCli.MoveMessage(ctx, inboxFolder, uid, dst)
		if err != nil {
			d.logger.Error("failed to move email",
				"index", i+1,
				"uid", uid,
				"from", from,
				"subject", subject,
				"dst", dst,
				"error", err)
			continue
		}

		movedCount++
		d.logger.Info("screened email",
			"index", i+1,
			"total", len(moves),
			"from", from,
			"subject", subject,
			"dst", dst)
	}

	d.logger.Info("screening complete", "moved", movedCount, "total", len(moves))
	return nil
}
