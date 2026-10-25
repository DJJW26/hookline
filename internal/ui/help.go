package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/DJJW26/hookline/internal/style"
)

// helpModel renders the "?" overlay: every keyHint from keys.go in a
// two-column layout, centred. It holds no state; the app treats it as a
// pure view. Narrow terminals fall back to a single column.
type helpModel struct{}

// View renders the help panel sized to fit within width×height. When the
// rows cannot fit the height, blank separators go first, then the tail is
// cut with a pointer to the README — the overlay must never overflow the
// frame.
func (helpModel) View(st style.Styles, width, height int) string {
	keyW := 0
	for _, sec := range helpSections {
		for _, h := range sec.hints {
			if n := lipgloss.Width(h.key); n > keyW {
				keyW = n
			}
		}
	}
	rows := make([][]string, 0, len(helpSections))
	for _, sec := range helpSections {
		rows = append(rows, []string{st.HelpTitle.Render(sec.title)})
		for _, h := range sec.hints {
			pad := strings.Repeat(" ", keyW-lipgloss.Width(h.key))
			rows = append(rows, []string{
				"  " + st.HelpKey.Render(h.key) + pad + "  " + st.HelpAction.Render(h.action)})
		}
		rows = append(rows, []string{""})
	}
	if n := len(rows); n > 0 && rows[n-1][0] == "" {
		rows = rows[:n-1]
	}
	rows = fitHelpRows(st, rows, height-4) // minus panel padding (2) + border (2)

	var panel string
	if twoColumnFits(rows, width) {
		half := (len(rows) + 1) / 2
		left := columnBlock(rows[:half])
		right := columnBlock(rows[half:])
		panel = st.HelpPanel.Render(lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right))
	} else {
		panel = st.HelpPanel.Render(columnBlock(rows))
	}

	if width > 0 && height > 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, panel)
	}
	return panel
}

// fitHelpRows drops blank separators, then tail rows, until the help fits
// avail lines.
func fitHelpRows(st style.Styles, rows [][]string, avail int) [][]string {
	if avail < 3 {
		avail = 3
	}
	for len(rows) > avail {
		cut := false
		for i, r := range rows {
			if r[0] == "" {
				rows = append(rows[:i], rows[i+1:]...)
				cut = true
				break
			}
		}
		if cut {
			continue
		}
		rows = rows[:avail-1]
		rows = append(rows, []string{st.Empty.Render("… more in README")})
		break
	}
	return rows
}

// twoColumnFits reports whether splitting rows side-by-side stays within
// width, using the widest row as the column estimate.
func twoColumnFits(rows [][]string, width int) bool {
	if len(rows) < 6 {
		return false
	}
	widest := 0
	for _, r := range rows {
		if n := lipgloss.Width(r[0]); n > widest {
			widest = n
		}
	}
	// 2 columns + 3-space gap + panel padding (1,3) + border (2).
	return widest*2+3+8 <= width
}

func columnBlock(rows [][]string) string {
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(r[0])
	}
	return b.String()
}
