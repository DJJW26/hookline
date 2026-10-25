package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hookline/internal/delivery"
	"hookline/internal/style"
)

// Layout and top-level view composition.
//
// The screen is four bands: a one-line header (wordmark left, listener
// status right), the pane area (side-by-side on wide terminals, stacked on
// narrow ones), the inline input row, and a one-line status bar. Panels are
// always exactly as tall as their allotted block, so the frame never
// jitters between states.

// splitWidth is the minimum terminal width for side-by-side panes.
const splitWidth = 96

func (m *Model) splitPanes() bool { return m.width >= splitWidth }

// bodyHeight is the height available to panes: total minus header, input
// row and status line. Below 3 lines no pane fits (a bordered empty panel
// is already 3 tall); bodyView falls back to blanks.
func (m *Model) bodyHeight() int {
	h := m.height - 3
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) listWidth() int {
	w := m.width * 2 / 5
	if w < 30 {
		w = 30
	}
	if w > 52 {
		w = 52
	}
	if m.width > 0 && w > m.width-20 {
		w = m.width - 20
	}
	return w
}

func (m *Model) stackedListH() int {
	h := m.bodyHeight() * 2 / 5
	if h < 6 {
		h = 6
	}
	if maxH := m.bodyHeight() - 6; h > maxH {
		h = maxH
	}
	if h < 3 {
		h = 3
	}
	return h
}

// listContentH is the number of rows visible inside the list panel.
func (m *Model) listContentH() int {
	h := m.bodyHeight()
	if !m.splitPanes() {
		h = m.stackedListH()
	}
	return h - 3 // borders + title row
}

// View renders the whole frame.
func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return m.st.Brand.Render("hookline") + " starting…"
	}
	var b strings.Builder
	b.WriteString(m.headerLine())
	b.WriteString("\n")
	if m.mode == modeHelp && m.bodyHeight() >= 5 {
		b.WriteString(m.help.View(m.st, m.width, m.bodyHeight()))
	} else if m.mode == modeHelp {
		// Too short for even a trimmed overlay.
		b.WriteString(strings.Join([]string{blankLine(m.width), blankLine(m.width), blankLine(m.width), blankLine(m.width), blankLine(m.width)}[:m.bodyHeight()], "\n"))
	} else {
		b.WriteString(m.bodyView())
	}
	b.WriteString("\n")
	b.WriteString(m.inputLine())
	b.WriteString("\n")
	b.WriteString(m.statusLine())
	if m.pendingOSC != "" {
		b.WriteString(m.pendingOSC)
	}
	return b.String()
}

// headerLine: "◆ hookline" left, listener state right. Hard-truncated to
// the terminal width so tiny windows never overflow the frame.
func (m *Model) headerLine() string {
	left := m.st.Brand.Render(style.GlyphBrand + " hookline")
	var right string
	if m.cfg.ListenURL == "" {
		right = m.st.Idle.Render("offline — viewing history")
	} else {
		addr := strings.TrimPrefix(m.cfg.ListenURL, "http://")
		right = m.st.Live.Render(style.GlyphLive) + " " + m.st.HeaderID.Render("listening "+style.GlyphBullet+" "+addr)
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if m.width > 0 {
		line = m.st.TruncateEnd(line, m.width)
	}
	return line
}

// bodyView lays out the panes (or the diff review panel). The help overlay
// is rendered by View directly and never reaches here.
func (m *Model) bodyView() string {
	if m.bodyHeight() < 3 {
		// No pane fits: fill the block with blanks so the frame stays
		// exactly the terminal height.
		return strings.Join([]string{blankLine(m.width), blankLine(m.width), blankLine(m.width)}[:m.bodyHeight()], "\n")
	}
	if m.mode == modeDiff && m.pending != nil {
		return m.diffView()
	}
	ls := listState{
		items:     m.visible(),
		total:     len(m.items),
		filter:    m.filterLabel(),
		listenURL: m.cfg.ListenURL,
		replays:   m.replayStatuses(),
	}
	sel := m.selection()
	replayed := m.replayFor(sel)

	if m.splitPanes() {
		listW := m.listWidth()
		detailW := m.width - listW - 1
		listPane := m.list.View(m.st, ls, listW, m.bodyHeight(), m.focus == focusList)
		detailPane := m.detail.View(m.st, sel, replayed, detailW, m.bodyHeight(), m.focus == focusDetail)
		return lipgloss.JoinHorizontal(lipgloss.Top, listPane, " ", detailPane)
	}

	listH := m.stackedListH()
	detailH := m.bodyHeight() - listH - 1
	if detailH < 4 {
		// Too short to show two panes: the list gets the whole block.
		return m.list.View(m.st, ls, m.width, m.bodyHeight(), m.focus == focusList)
	}
	listPane := m.list.View(m.st, ls, m.width, listH, m.focus == focusList)
	detailPane := m.detail.View(m.st, sel, replayed, m.width, detailH, m.focus == focusDetail)
	return lipgloss.JoinVertical(lipgloss.Left, listPane, blankLine(m.width), detailPane)
}

// replayStatuses projects replayByID into the ID→status map the list
// badges consume.
func (m *Model) replayStatuses() map[string]int {
	out := make(map[string]int, len(m.replayByID))
	for id, info := range m.replayByID {
		out[id] = info.res.StatusCode
	}
	return out
}

// replayFor returns the replay outcome attached to sel, if any.
func (m *Model) replayFor(sel *delivery.Delivery) *replayInfo {
	if sel == nil {
		return nil
	}
	if info, ok := m.replayByID[sel.ID]; ok {
		return &info
	}
	return nil
}

// diffView replaces the detail pane while reviewing an edited payload.
func (m *Model) diffView() string {
	p := m.pending
	title := " diff review"
	note := m.st.PanelNote.Render(m.st.TruncateEnd(p.next.Method+" "+p.next.URL(), 48))
	inner := m.width - 2
	titleLine := title + strings.Repeat(" ", max(0, inner-lipgloss.Width(title)-lipgloss.Width(note))) + note
	content := renderDiff(m.st, p.orig.Body, p.next.Body, inner)
	lines := strings.Split(content, "\n")
	viewH := m.bodyHeight() - 3
	for len(lines) < viewH {
		lines = append(lines, blankLine(inner))
	}
	if len(lines) > viewH {
		lines = lines[:viewH]
	}
	body := strings.Join(append([]string{titleLine}, lines...), "\n")
	return m.st.PanelFocus.Render(body)
}

// inputLine shows the active prompt, or reserves its row silently.
func (m *Model) inputLine() string {
	if m.mode == modeSearch || m.mode == modeTarget {
		m.input.width = m.width
		return m.input.View(m.st)
	}
	return blankLine(m.width)
}

// statusLine: contextual hints left, version and count right. A toast
// temporarily replaces the hints.
func (m *Model) statusLine() string {
	left := m.hints()
	if t := m.toast; t != nil {
		left = m.renderToast(*t)
	}
	// The right side always shows where r would send: the target deserves
	// to be visible, not remembered.
	rightParts := []string{"v" + m.cfg.Version}
	if m.target != "" {
		rightParts = append(rightParts, "→ "+strings.TrimPrefix(m.target, "http://"))
	}
	rightParts = append(rightParts, fmt.Sprintf("%d deliveries", len(m.items)))
	right := m.st.StatusDim.Render(strings.Join(rightParts, " "+style.GlyphBullet+" "))
	if lipgloss.Width(right) >= m.width {
		// Ultra-narrow terminals: shrink the right side before clamping.
		right = m.st.TruncateEnd(right, max(8, m.width-lipgloss.Width(left)-1))
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
		if w := m.width - lipgloss.Width(right) - 1; w > 0 {
			left = m.st.TruncateEnd(left, w)
		}
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) renderToast(t toast) string {
	s := m.st.Status
	switch t.kind {
	case toastGood:
		s = m.st.ToastGood
	case toastWarn:
		s = m.st.ToastWarn
	case toastBad:
		s = m.st.ToastBad
	}
	text := t.text
	if t.subject != "" {
		text = t.subject + " — " + text
	}
	return s.Render(text)
}

// hints are the mode-aware keycaps on the status line.
func (m *Model) hints() string {
	switch m.mode {
	case modeConfirm:
		return m.st.ToastWarn.Render("clear all deliveries? ") +
			m.st.StatusKey.Render("y") + m.st.Status.Render(" confirm ") +
			m.st.StatusKey.Render("esc") + m.st.Status.Render(" cancel")
	case modeDiff:
		return m.st.StatusKey.Render("s") + m.st.Status.Render(" send edited ") +
			m.st.StatusKey.Render("esc") + m.st.Status.Render(" discard")
	case modeSearch:
		return m.st.Status.Render("type to filter ") +
			m.st.StatusKey.Render("enter") + m.st.Status.Render(" apply ") +
			m.st.StatusKey.Render("esc") + m.st.Status.Render(" clear")
	case modeTarget:
		return m.st.Status.Render("replay target, e.g. http://localhost:3000 ")
	}

	pairs := []struct{ k, a string }{}
	if m.focus == focusDetail {
		pairs = append(pairs,
			struct{ k, a string }{"h l", "tabs"},
			struct{ k, a string }{"j k", "scroll"},
			struct{ k, a string }{"esc", "back"},
			struct{ k, a string }{"r", "replay"},
			struct{ k, a string }{"e", "edit"},
		)
	} else {
		pairs = append(pairs,
			struct{ k, a string }{"j k", "select"},
			struct{ k, a string }{"tab", "pane"},
			struct{ k, a string }{"r", "replay"},
			struct{ k, a string }{"e", "edit"},
			struct{ k, a string }{"c", "curl"},
			struct{ k, a string }{"/", "find"},
		)
	}
	pairs = append(pairs, struct{ k, a string }{"?", "help"})

	var parts []string
	for _, p := range pairs {
		parts = append(parts, m.st.StatusKey.Render(p.k)+m.st.Status.Render(" "+p.a))
	}
	return m.st.Status.Render(strings.Join(parts, " · "))
}
