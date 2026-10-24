package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// Clipboard support that blends into whatever terminal the user runs:
//
//   - Default: OSC 52. The copy is performed by the terminal itself, so it
//     works identically on a local Windows Terminal, iTerm2, kitty, WezTerm,
//     GNOME Terminal, and inside SSH sessions — the bytes never leave the
//     terminal's own clipboard facility.
//   - Escape hatch: HOOKLINE_CLIP_CMD (e.g. "clip.exe" or "wl-copy") is
//     executed with the text on stdin for terminals without OSC 52.
//
// OSC 52 bytes cannot be written directly from a command goroutine without
// racing the renderer, so they travel through oscMsg and View emits them
// exactly once, then a flush message clears them.

const clipSeqEnv = "HOOKLINE_CLIP_CMD"

// copyText returns a command that places s on the clipboard and reports the
// outcome as a toast through clipDoneMsg.
func copyText(s string, what string) tea.Cmd {
	if external := strings.TrimSpace(os.Getenv(clipSeqEnv)); external != "" {
		return runExternalClip(external, s, what)
	}
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(s)))
	return func() tea.Msg {
		return oscMsg(seq)
	}
}

type clipDoneMsg struct {
	err  error
	what string
}

func runExternalClip(command, s, what string) tea.Cmd {
	return func() tea.Msg {
		fields := strings.Fields(command)
		cmd := exec.Command(fields[0], fields[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err != nil {
			return clipDoneMsg{err: fmt.Errorf("%s: %w", command, err), what: what}
		}
		return clipDoneMsg{what: what}
	}
}
