// Command neomd is a minimal Neovim-flavored Markdown email client.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/config"
	"github.com/sspaeti/neomd/internal/daemon"
	goIMAP "github.com/sspaeti/neomd/internal/imap"
	"github.com/sspaeti/neomd/internal/oauth2"
	"github.com/sspaeti/neomd/internal/screener"
	"github.com/sspaeti/neomd/internal/ui"
)

// version is set at build time via -ldflags "-X main.version=v0.1.0"
var version = "dev"

func main() {
	// Go's standard flag parser stops at the first positional argument. Pull
	// -config out first so the documented `neomd read <link> --config ...`
	// spelling works as well as the traditional global-flag spelling.
	os.Args = append([]string{os.Args[0]}, moveConfigArgsToFront(os.Args[1:])...)
	cfgPath := flag.String("config", "", "path to config.toml (default: ~/.config/neomd/config.toml)")
	showVersion := flag.Bool("version", false, "print version and exit")
	headless := flag.Bool("headless", false, "run in headless daemon mode (no TUI)")
	mailtoFlag := flag.String("mailto", "", "open compose with a mailto: URI (e.g. mailto:user@example.com?subject=Hello)")
	flag.Parse()

	// Also accept mailto: URI as a positional argument (for xdg-open / .desktop handler).
	mailtoURI := *mailtoFlag
	if mailtoURI == "" && flag.NArg() > 0 && strings.HasPrefix(flag.Arg(0), "mailto:") {
		mailtoURI = flag.Arg(0)
	}

	if *showVersion {
		fmt.Println("neomd", version)
		return
	}
	readCommand := flag.NArg() > 0 && flag.Arg(0) == "read"
	legacyRead := readCommand && hasArg(flag.Args()[1:], "--uid")
	var readOptions agentReadOpts
	var readOptionsErr error
	if readCommand && !legacyRead {
		readOptions, readOptionsErr = parseAgentReadArgs(flag.Args()[1:])
		if readOptionsErr != nil {
			fmt.Fprintf(os.Stderr, "neomd read: %v\n", readOptionsErr)
			os.Exit(2)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		if strings.Contains(err.Error(), "please fill in") {
			fmt.Fprintln(os.Stderr, "neomd:", err)
			if readCommand {
				os.Exit(2)
			}
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "neomd: config error: %v\n", err)
		if readCommand {
			os.Exit(2)
		}
		os.Exit(1)
	}
	if *headless && cfg.ReadOnly {
		fmt.Fprintln(os.Stderr, "neomd: --headless is disabled by read_only mode")
		os.Exit(1)
	}

	accounts := cfg.ActiveAccounts()
	if readCommand && readOptions.account != "" {
		var selected []config.AccountConfig
		for _, account := range accounts {
			if strings.EqualFold(account.Name, readOptions.account) {
				selected = append(selected, account)
			}
		}
		if len(selected) == 0 {
			fmt.Fprintf(os.Stderr, "neomd read: unknown account %q\n", readOptions.account)
			os.Exit(2)
		}
		accounts = selected
	}
	if len(accounts) == 0 {
		fmt.Fprintln(os.Stderr, "neomd: no accounts configured in config.toml")
		if readCommand {
			os.Exit(2)
		}
		os.Exit(1)
	}

	// Build one IMAP client per account (nil for imap_disabled accounts).
	imapClients := make([]*goIMAP.Client, 0, len(accounts))
	for _, acc := range accounts {
		if acc.IMAPDisabled {
			imapClients = append(imapClients, nil)
			continue
		}
		h, p := splitAddr(acc.IMAP)
		// Determine TLS/STARTTLS: respect explicit user config, otherwise infer from port.
		// Security: non-standard ports default to TLS (e.g., Proton Mail Bridge on 1143).
		useTLS, useSTARTTLS := inferIMAPSecurity(p, acc.STARTTLS)
		imapCfg := goIMAP.Config{
			Host:        h,
			Port:        p,
			User:        acc.User,
			Password:    acc.Password,
			TLS:         useTLS,
			STARTTLS:    useSTARTTLS,
			TLSCertFile: acc.TLSCertFile,
		}
		if err := configureIMAPAuth(ctx, acc, &imapCfg); err != nil {
			fmt.Fprintf(os.Stderr, "neomd: account %q: %v\n", acc.Name, err)
			if readCommand {
				os.Exit(2)
			}
			os.Exit(1)
		}
		// The script-facing read command is always EXAMINE/BODY.PEEK, even
		// when the user's normal TUI profile is writable.
		imapCfg.ReadOnly = cfg.ReadOnly || readCommand
		imapClients = append(imapClients, goIMAP.New(imapCfg))
	}
	defer func() {
		for _, c := range imapClients {
			if c != nil {
				c.Close()
			}
		}
	}()

	// `neomd list ...` — read-only JSON dump of folder headers for external
	// widgets (e.g. the omarchy bar plugin). Uses the first IMAP-enabled
	// account, prints one JSON object, exits 0 even on failure.
	if flag.NArg() > 0 && flag.Arg(0) == "list" {
		var listCli *goIMAP.Client
		accName := ""
		for i, c := range imapClients {
			if c != nil {
				listCli = c
				accName = accounts[i].Name
				break
			}
		}
		if listCli == nil {
			writeListJSON(os.Stdout, listOutput{Error: "no IMAP-enabled account configured"})
			os.Exit(0)
		}
		code := runList(ctx, cfg.Folders, accName, listCli, flag.Args()[1:], os.Stdout)
		listCli.Close()
		os.Exit(code)
	}

	// `neomd read <link>` — resolve a Message-ID across the selected account(s)
	// without mutating mailboxes. Legacy --folder/--uid remains for the widget.
	if flag.NArg() > 0 && flag.Arg(0) == "read" {
		if legacyRead {
			var readCli *goIMAP.Client
			for _, c := range imapClients {
				if c != nil {
					readCli = c
					break
				}
			}
			if readCli == nil {
				writeReadJSON(os.Stdout, readOutput{Error: "no IMAP-enabled account configured"})
				os.Exit(0)
			}
			code := runRead(ctx, cfg.Folders, readCli, flag.Args()[1:], os.Stdout)
			readCli.Close()
			os.Exit(code)
		}
		readClients := make([]readClient, 0, len(accounts))
		for i, client := range imapClients {
			if client != nil {
				readClients = append(readClients, readClient{account: accounts[i].Name, client: client})
			}
		}
		if len(readClients) == 0 {
			fmt.Fprintln(os.Stderr, "neomd read: no IMAP-enabled account configured")
			os.Exit(2)
		}
		code := runAgentRead(ctx, cfg.Folders, readClients, readOptions, os.Stdin, os.Stdout, os.Stderr)
		os.Exit(code)
	}

	// Screener (shared across accounts — same allowlist files).
	sc, err := screener.New(screener.Config{
		ScreenedIn:  cfg.Screener.ScreenedIn,
		ScreenedOut: cfg.Screener.ScreenedOut,
		Feed:        cfg.Screener.Feed,
		PaperTrail:  cfg.Screener.PaperTrail,
		Spam:        cfg.Screener.Spam,
		Notify:      cfg.Screener.Notify,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "neomd: screener error: %v\n", err)
		os.Exit(1)
	}

	// `neomd screen --from <addr> --action in|out|feed|paper` — classify a
	// sender from an external widget: screener list update + sender-level
	// ToScreen move, same semantics as the TUI's I/O/F/P keys.
	if flag.NArg() > 0 && flag.Arg(0) == "screen" {
		if cfg.ReadOnly {
			writeScreenJSON(os.Stdout, screenOutput{Error: "neomd read-only mode: screener action blocked"})
			os.Exit(0)
		}
		var screenCli *goIMAP.Client
		for _, c := range imapClients {
			if c != nil {
				screenCli = c
				break
			}
		}
		if screenCli == nil {
			writeScreenJSON(os.Stdout, screenOutput{Error: "no IMAP-enabled account configured"})
			os.Exit(0)
		}
		code := runScreen(ctx, cfg.Folders, sc, screenCli, flag.Args()[1:], os.Stdout)
		screenCli.Close()
		os.Exit(code)
	}

	// Fork: run either headless daemon or TUI
	if *headless {
		// Headless daemon mode: run background screening loop. Find the first
		// IMAP-enabled account — imap_disabled accounts have a nil client and
		// would crash the daemon's screening loop.
		var daemonCli *goIMAP.Client
		for _, c := range imapClients {
			if c != nil {
				daemonCli = c
				break
			}
		}
		if daemonCli == nil {
			fmt.Fprintln(os.Stderr, "neomd: --headless requires at least one IMAP-enabled account")
			os.Exit(1)
		}
		d := daemon.New(*cfg, daemonCli, sc)
		if err := d.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "neomd: daemon error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// TUI mode: run interactive interface
		ui.Version = version
		var mailto *ui.MailtoParams
		if mailtoURI != "" {
			mailto = parseMailto(mailtoURI)
		}
		model := ui.New(cfg, imapClients, sc, mailto)

		p := tea.NewProgram(
			model,
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "neomd: %v\n", err)
			os.Exit(1)
		}
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want || strings.HasPrefix(arg, want+"=") {
			return true
		}
	}
	return false
}

func moveConfigArgsToFront(args []string) []string {
	result := make([]string, 0, len(args)+2)
	var configArgs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" || args[i] == "-config" {
			if i+1 < len(args) {
				configArgs = []string{"--config", args[i+1]}
				i++
			}
			continue
		}
		if strings.HasPrefix(args[i], "--config=") || strings.HasPrefix(args[i], "-config=") {
			configArgs = []string{"--config", strings.SplitN(args[i], "=", 2)[1]}
			continue
		}
		result = append(result, args[i])
	}
	return append(configArgs, result...)
}

func configureIMAPAuth(ctx context.Context, acc config.AccountConfig, imapCfg *goIMAP.Config) error {
	if len(acc.OAuth2TokenCommand) > 0 {
		imapCfg.TokenSource = oauth2.CommandTokenSource(acc.OAuth2TokenCommand)
		return nil
	}
	if acc.IsOAuth2() {
		if acc.OAuth2ClientID == "" {
			return fmt.Errorf("oauth2_client_id is required")
		}
		if acc.OAuth2IssuerURL == "" && (acc.OAuth2AuthURL == "" || acc.OAuth2TokenURL == "") {
			return fmt.Errorf("set oauth2_issuer_url or both oauth2_auth_url and oauth2_token_url")
		}
		tokenFile, err := config.TokenFilePath(acc.Name)
		if err != nil {
			return err
		}
		ts, err := oauth2.TokenSource(ctx, oauth2.Config{
			ClientID:     acc.OAuth2ClientID,
			ClientSecret: acc.OAuth2ClientSecret,
			IssuerURL:    acc.OAuth2IssuerURL,
			AuthURL:      acc.OAuth2AuthURL,
			TokenURL:     acc.OAuth2TokenURL,
			Scopes:       acc.OAuth2Scopes,
			RedirectPort: acc.OAuth2RedirectPort,
			TokenFile:    tokenFile,
			AccountName:  acc.Name,
		})
		if err != nil {
			return fmt.Errorf("oauth2: %w", err)
		}
		imapCfg.TokenSource = ts
		return nil
	}
	if acc.User == "" || acc.Password == "" {
		return fmt.Errorf("user/password not set")
	}
	return nil
}

func splitAddr(addr string) (host, port string) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, "993"
	}
	return addr[:i], addr[i+1:]
}

// inferIMAPSecurity determines TLS/STARTTLS settings based on port and user config.
// Returns (useTLS, useSTARTTLS).
//
// Logic:
//   - If userSTARTTLS is true: always use STARTTLS (user explicitly enabled it)
//   - Standard ports: 993 → TLS, 143 → STARTTLS
//   - Non-standard ports: default to TLS (e.g., Proton Mail Bridge on 1143)
//
// parseMailto parses a mailto: URI into MailtoParams.
// Format: mailto:addr?subject=S&cc=C&bcc=B&body=B
func parseMailto(raw string) *ui.MailtoParams {
	// url.Parse chokes on mailto: without //, so fix up.
	u, err := url.Parse(raw)
	if err != nil {
		return &ui.MailtoParams{To: raw}
	}
	to := u.Opaque // everything before ?
	if to == "" {
		to = u.Path
	}
	// Percent-decode the "to" field (some mailers encode spaces/commas).
	if decoded, err := url.PathUnescape(to); err == nil {
		to = decoded
	}
	// Parse query manually: url.Query() decodes '+' as space, but in
	// mailto: URIs '+' is a literal character (RFC 6068).
	q := parseMailtoQuery(u.RawQuery)
	return &ui.MailtoParams{
		To:      to,
		CC:      q("cc"),
		BCC:     q("bcc"),
		Subject: q("subject"),
		Body:    q("body"),
	}
}

// parseMailtoQuery parses a raw query string using PathUnescape (not
// QueryUnescape) so that literal '+' characters are preserved per RFC 6068.
func parseMailtoQuery(raw string) func(string) string {
	m := make(map[string]string)
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		if dk, err := url.PathUnescape(k); err == nil {
			k = dk
		}
		if dv, err := url.PathUnescape(v); err == nil {
			v = dv
		}
		k = strings.ToLower(k)
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return func(key string) string { return m[key] }
}

func inferIMAPSecurity(port string, userSTARTTLS bool) (useTLS, useSTARTTLS bool) {
	return goIMAP.InferSecurity(port, userSTARTTLS)
}
