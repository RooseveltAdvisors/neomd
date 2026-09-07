package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Clipboard ────────────────────────────────────────────────────────────
//
// neomd is routinely run over ssh (often inside tmux) while the user pastes in
// their own terminal emulator. A local clipboard tool cannot serve that: on a
// headless host xclip/xsel/wl-copy either fail outright (no DISPLAY) or set the
// clipboard of the wrong machine. OSC 52 writes to the terminal the user is
// actually looking at, so it is the primary path; the local tools stay as a
// fallback for terminals that do not implement OSC 52.

// clipboardDoneMsg reports the result of a clipboard copy.
type clipboardDoneMsg struct {
	text string
	err  error
}

// copyToClipboardCmd copies text to the clipboard the user pastes from.
func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		return clipboardDoneMsg{text: text, err: copyToClipboard(text)}
	}
}

// copyToClipboard sends text to the terminal via OSC 52 and, when a local
// graphical session is reachable, also to a local clipboard tool. Either one
// succeeding is a success.
func copyToClipboard(text string) error {
	oscErr := writeOSC52(text)
	toolErr := copyViaLocalTool(text)
	if oscErr == nil || toolErr == nil {
		return nil
	}
	return fmt.Errorf("%v; %v", oscErr, toolErr)
}

// osc52Sequence builds the OSC 52 "set system clipboard" sequence. Inside tmux
// the same payload is repeated wrapped in a DCS passthrough (ESC doubled, as
// the passthrough requires), so the copy lands whether the user's tmux forwards
// OSC 52 itself (set-clipboard on) or only passes it through
// (allow-passthrough on). Whichever form that tmux does not honour is consumed
// silently, and setting the same text twice is harmless.
func osc52Sequence(text string, inTmux bool) string {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	if inTmux {
		seq += "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

// writeOSC52 emits the escape sequence on stdout, which is the terminal
// bubbletea renders to. The sequence moves no cursor and paints no cell, so it
// is safe to interleave with a render frame. When stdout is redirected it falls
// back to the controlling terminal.
func writeOSC52(text string) error {
	seq := osc52Sequence(text, os.Getenv("TMUX") != "")
	if info, err := os.Stdout.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		_, err := os.Stdout.WriteString(seq)
		return err
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("no terminal to copy through: %w", err)
	}
	defer tty.Close()
	_, err = tty.WriteString(seq)
	return err
}

// clipboardTools are tried in order. needEnv names the environment variable
// that must be set for the tool to reach a display; empty means always usable.
var clipboardTools = []struct {
	argv    []string
	needEnv string
}{
	{argv: []string{"wl-copy"}, needEnv: "WAYLAND_DISPLAY"},
	{argv: []string{"xclip", "-selection", "clipboard"}, needEnv: "DISPLAY"},
	{argv: []string{"xsel", "--clipboard", "--input"}, needEnv: "DISPLAY"},
	{argv: []string{"pbcopy"}}, // macOS
}

func copyViaLocalTool(text string) error {
	for _, t := range clipboardTools {
		if t.needEnv != "" && os.Getenv(t.needEnv) == "" {
			continue
		}
		if _, err := exec.LookPath(t.argv[0]); err != nil {
			continue
		}
		cmd := exec.Command(t.argv[0], t.argv[1:]...)
		cmd.Stdin = strings.NewReader(text)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w: %s", t.argv[0], err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}
	return fmt.Errorf("no usable clipboard tool (wl-copy, xclip, xsel, pbcopy)")
}
