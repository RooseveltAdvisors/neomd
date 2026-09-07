package ui

import (
	"encoding/base64"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sspaeti/neomd/internal/imap"
)

// The reader yank menu must put the Message-ID link on the clipboard the user
// pastes from. neomd is commonly run over ssh, where no local clipboard tool can
// reach that terminal — so the copy has to go out as OSC 52.
func TestYankMenuMessageIDIsOSC52Encoded(t *testing.T) {
	m := Model{
		state:     stateReading,
		openEmail: &imap.Email{MessageID: "<abc@example.com>"},
	}
	opened, _ := m.updateReader(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	_, cmd := opened.(Model).updateCopyMenu(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})

	msg, ok := cmd().(clipboardDoneMsg)
	if !ok {
		t.Fatalf("copy command returned %T, want clipboardDoneMsg", msg)
	}
	want := "neomd://mid/%3Cabc@example.com%3E"
	if msg.text != want {
		t.Fatalf("copied text = %q, want %q", msg.text, want)
	}

	seq := osc52Sequence(msg.text, false)
	payload := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\a")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("OSC 52 payload is not base64: %v (seq %q)", err, seq)
	}
	if string(decoded) != want {
		t.Fatalf("OSC 52 carries %q, want %q", decoded, want)
	}
}

func TestOSC52TmuxPassthroughDoublesEscapes(t *testing.T) {
	plain := osc52Sequence("hi", false)
	if plain != "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte("hi"))+"\a" {
		t.Fatalf("plain sequence = %q", plain)
	}

	// Inside tmux both forms are emitted: the plain one (honoured when tmux has
	// set-clipboard on) and a DCS passthrough (honoured with allow-passthrough on).
	wrapped := osc52Sequence("hi", true)
	rest := strings.TrimPrefix(wrapped, plain)
	if rest == wrapped {
		t.Fatalf("tmux sequence does not start with the plain sequence: %q", wrapped)
	}
	if !strings.HasPrefix(rest, "\x1bPtmux;") || !strings.HasSuffix(rest, "\x1b\\") {
		t.Fatalf("tmux passthrough not wrapped in DCS: %q", rest)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(rest, "\x1bPtmux;"), "\x1b\\")
	if inner != strings.ReplaceAll(plain, "\x1b", "\x1b\x1b") {
		t.Fatalf("tmux passthrough must double every ESC, got %q", inner)
	}
}

// A local clipboard tool that cannot reach a display must be skipped rather than
// run and failed — that is what silently swallowed the copy on a headless host.
func TestLocalClipboardToolSkippedWithoutDisplay(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	for _, tool := range clipboardTools {
		if tool.needEnv == "" {
			continue
		}
		if err := copyViaLocalTool("x"); err == nil || !strings.Contains(err.Error(), "no usable clipboard tool") {
			t.Fatalf("copyViaLocalTool with no display = %v, want the no-usable-tool error", err)
		}
		break
	}
}
