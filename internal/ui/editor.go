package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbletea"

	"hookline/internal/delivery"
)

// The edit-and-resend flow: the payload round-trips through the user's real
// editor ($EDITOR / $VISUAL, notepad on Windows, vi elsewhere), then comes
// back as an editorDoneMsg and enters diff review.

// openEditor writes d's body to a temp file, suspends the TUI, and runs the
// user's editor on it. The command it returns completes when the editor
// exits, carrying the (possibly edited) file contents.
func openEditor(d delivery.Delivery) tea.Cmd {
	ext := ".txt"
	if d.Kind() == delivery.KindJSON {
		ext = ".json"
	}
	tmp, err := os.CreateTemp("", "hookline-*"+ext)
	if err != nil {
		return func() tea.Msg {
			return editorDoneMsg{d: d, err: err}
		}
	}
	if _, err := tmp.Write(d.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return func() tea.Msg {
			return editorDoneMsg{d: d, err: err}
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return func() tea.Msg {
			return editorDoneMsg{d: d, err: err}
		}
	}

	name := tmp.Name()
	fields := strings.Fields(resolveEditor())
	cmd := exec.Command(fields[0], append(fields[1:], name)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		body, readErr := os.ReadFile(name)
		if removeErr := os.Remove(name); err == nil {
			err = removeErr
		}
		if err == nil {
			err = readErr
		}
		return editorDoneMsg{d: d, body: body, err: err}
	})
}

// resolveEditor picks an editor: $EDITOR, then $VISUAL, then the platform
// default. The value may include arguments ("code --wait").
func resolveEditor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}
