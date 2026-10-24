package ui

import (
	"strings"

	"github.com/charmbracelet/bubbletea"

	"hookline/internal/style"
)

// inputModel is a minimal single-line text input. It exists so the project
// carries no widget-library dependency for two prompts; it renders with the
// same theme styles as everything else.
type inputModel struct {
	label  string // prompt label, e.g. "find"
	value  string
	cursor int // rune offset into value
	width  int
}

func newInput(label, initial string) inputModel {
	return inputModel{label: label, value: initial, cursor: len([]rune(initial))}
}

// Update consumes a key. It reports submit=true when Enter was pressed,
// cancel=true for Esc.
func (in *inputModel) Update(msg tea.Msg) (submit, cancel bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, false
	}
	runes := []rune(in.value)
	switch key.Type {
	case tea.KeyEnter:
		return true, false
	case tea.KeyEsc:
		return false, true
	case tea.KeyBackspace:
		if in.cursor > 0 {
			runes = append(runes[:in.cursor-1], runes[in.cursor:]...)
			in.cursor--
		}
	case tea.KeyDelete:
		if in.cursor < len(runes) {
			runes = append(runes[:in.cursor], runes[in.cursor+1:]...)
		}
	case tea.KeyLeft:
		if in.cursor > 0 {
			in.cursor--
		}
	case tea.KeyRight:
		if in.cursor < len(runes) {
			in.cursor++
		}
	case tea.KeyHome, tea.KeyCtrlA:
		in.cursor = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		in.cursor = len(runes)
	case tea.KeyCtrlU:
		runes = nil
		in.cursor = 0
	case tea.KeyRunes, tea.KeySpace:
		r := key.Runes
		if key.Type == tea.KeySpace {
			r = []rune{' '}
		}
		next := make([]rune, 0, len(runes)+len(r))
		next = append(next, runes[:in.cursor]...)
		next = append(next, r...)
		next = append(next, runes[in.cursor:]...)
		runes = next
		in.cursor += len(r)
	default:
		return false, false
	}
	in.value = string(runes)
	return false, false
}

// Value returns the current text.
func (in inputModel) Value() string { return in.value }

// View renders "label ▸ value▮" with the cursor as a reversed block.
func (in inputModel) View(st style.Styles) string {
	runes := []rune(in.value)
	if in.cursor > len(runes) {
		in.cursor = len(runes)
	}
	before := string(runes[:in.cursor])
	at := " "
	if in.cursor < len(runes) && runes[in.cursor] != ' ' {
		at = string(runes[in.cursor])
	}
	after := ""
	if in.cursor < len(runes) {
		after = string(runes[in.cursor+1:])
	}
	line := st.InputLabel.Render(in.label) + " " +
		st.InputText.Render(before) +
		st.InputCursor.Render(at) +
		st.InputText.Render(after)
	if in.width > 0 {
		line = st.TruncateEnd(line, in.width)
	}
	return line
}

// padding produces a blank line of the same height as the input row so the
// layout does not jump when a prompt opens.
func blankLine(width int) string {
	if width <= 0 {
		return ""
	}
	return strings.Repeat(" ", width)
}
