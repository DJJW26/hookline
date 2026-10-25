package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hookline/internal/delivery"
	"hookline/internal/style"
)

// listModel is the deliveries pane. Selection is tracked by delivery ID, not
// index, so newly arriving deliveries (inserted at the top) never yank the
// selection out from under the user.
type listModel struct {
	offset     int
	selectedID string
	unread     map[string]bool
}

func newListModel() listModel {
	return listModel{unread: map[string]bool{}}
}

// listState bundles what a render needs from the app: the visible (already
// filtered) items plus display context.
type listState struct {
	items     []*delivery.Delivery
	total     int    // unfiltered count
	filter    string // "" or a live filter description
	listenURL string
	replays   map[string]int // delivery ID → last replay status, for row badges
}

// markUnread flags a delivery as new.
func (l *listModel) markUnread(id string) { l.unread[id] = true }

// selectedIndex resolves the selectedID against the visible items, falling
// back to the newest row and clearing its unread flag.
func (l *listModel) selectedIndex(items []*delivery.Delivery) int {
	if len(items) == 0 {
		return -1
	}
	for i, d := range items {
		if d.ID == l.selectedID {
			delete(l.unread, d.ID)
			return i
		}
	}
	l.selectedID = items[0].ID
	delete(l.unread, items[0].ID)
	return 0
}

// move shifts the selection by delta (negative = up), keeping it inside the
// list and the visible window.
func (l *listModel) move(items []*delivery.Delivery, delta, contentH int) {
	if len(items) == 0 {
		return
	}
	idx := l.selectedIndex(items)
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx > len(items)-1 {
		idx = len(items) - 1
	}
	l.selectedID = items[idx].ID
	delete(l.unread, items[idx].ID)
	l.scrollTo(idx, len(items), contentH)
}

// jumpTo selects the top (0) or bottom (-1) of the list.
func (l *listModel) jumpTo(items []*delivery.Delivery, bottom bool, contentH int) {
	if len(items) == 0 {
		return
	}
	idx := 0
	if bottom {
		idx = len(items) - 1
	}
	l.selectedID = items[idx].ID
	delete(l.unread, items[idx].ID)
	l.scrollTo(idx, len(items), contentH)
}

// scrollTo adjusts the offset so idx is inside the visible window.
func (l *listModel) scrollTo(idx, total, contentH int) {
	if contentH <= 0 {
		contentH = 1
	}
	maxOff := total - contentH
	if maxOff < 0 {
		maxOff = 0
	}
	if l.offset > maxOff {
		l.offset = maxOff
	}
	if idx < l.offset {
		l.offset = idx
	}
	if idx >= l.offset+contentH {
		l.offset = idx - contentH + 1
	}
}

// wheel scrolls the viewport without moving the selection when it is at an
// edge, mirroring how scroll views behave elsewhere.
func (l *listModel) wheel(items []*delivery.Delivery, up bool, contentH int) {
	total := len(items)
	maxOff := total - contentH
	if maxOff < 0 {
		maxOff = 0
	}
	if up {
		l.offset--
	} else {
		l.offset++
	}
	if l.offset < 0 {
		l.offset = 0
	}
	if l.offset > maxOff {
		l.offset = maxOff
	}
	// If the selection scrolled out of view, pull it to the nearest edge.
	if total > 0 {
		idx := l.selectedIndex(items)
		if idx < l.offset {
			l.selectedID = items[l.offset].ID
		}
		if idx >= l.offset+contentH {
			l.selectedID = items[min(l.offset+contentH-1, total-1)].ID
		}
	}
}

// View renders the whole list panel: border, title row and rows.
func (l *listModel) View(st style.Styles, s listState, w, h int, focused bool) string {
	if w < 8 || h < 4 {
		return st.Panel.Render("")
	}
	panel := st.Panel
	if focused {
		panel = st.PanelFocus
	}

	inner := w - 2 // rounded border sides
	title := " deliveries"
	note := listNote(s)
	titleLine := title + strings.Repeat(" ", max(0, inner-lipgloss.Width(title)-lipgloss.Width(note))) + note

	contentH := h - 3 // borders + title row
	var rows []string
	if len(s.items) == 0 {
		rows = emptyList(st, s, inner)
	} else {
		sel := l.selectedIndex(s.items) // normalise selection / clear unread
		// Filtering can shrink the list below the scroll offset; clamp
		// here so the pane never renders blank.
		if maxOff := max(0, len(s.items)-contentH); l.offset > maxOff {
			l.offset = maxOff
		}
		end := min(l.offset+contentH, len(s.items))
		for i := l.offset; i < end; i++ {
			rows = append(rows, renderRow(st, s.items[i], i == sel, focused, l.unread[s.items[i].ID], s.replays[s.items[i].ID], inner))
		}
	}
	body := strings.Join(rows, "\n")
	content := lipgloss.JoinVertical(lipgloss.Left, titleLine, body)

	// Content is exactly h-2 rows: pad when short, trim when long (the
	// empty state or a dense burst must never grow the panel past its
	// allotted block — scrolling handles overflow).
	lines := strings.Split(content, "\n")
	for len(lines) < h-2 {
		lines = append(lines, blankLine(inner))
	}
	if len(lines) > h-2 {
		lines = lines[:h-2]
	}
	return panel.Render(strings.Join(lines, "\n"))
}

func listNote(s listState) string {
	parts := []string{fmt.Sprintf("%d", s.total)}
	if s.filter != "" {
		parts = append(parts, s.filter)
	}
	return strings.Join(parts, " · ")
}

func renderRow(st style.Styles, d *delivery.Delivery, selected, focused, isNew bool, replayStatus int, width int) string {
	row := renderRowContent(st, d, isNew, replayStatus, width)
	if selected {
		if focused {
			row = st.RowSelectedLive.Render(row)
		} else {
			row = st.RowSelected.Render(row)
		}
	}
	// Ultra-narrow panes: the fixed columns alone may exceed the width;
	// cut the row rather than the frame.
	return st.TruncateEnd(row, width)
}

func renderRowContent(st style.Styles, d *delivery.Delivery, isNew bool, replayStatus int, width int) string {
	markCol := " "
	if isNew {
		markCol = st.Unread.Render(style.GlyphUnread)
	}
	timeCol := st.RowTime.Render(dayTime(d.TS))
	method := st.Method(d.Method).Render(methodLabel(d.Method))

	meta := strings.TrimSpace(humanSize(d.Size()) + " " + kindLabel(*d))
	if replayStatus != 0 {
		// The last replay outcome rides along in the meta column, colored
		// by status class — the "which of these pass?" affordance.
		meta = fmt.Sprintf("%d %s", replayStatus, meta)
	}
	metaCol := st.RowDim.Render(meta)
	if replayStatus != 0 {
		metaCol = st.StatusStyle(replayStatus).Render(meta)
	}

	// Reserve: 3 leading, time, 6 method, three 1-col separators, meta,
	// 1 trailing — path gets whatever remains (min 4). Time width varies
	// (today vs older dates), so measure it rather than assume 8.
	pathBudget := width - (3 + lipgloss.Width(timeCol) + 1 + lipgloss.Width(method) + 1 + 1 + lipgloss.Width(metaCol) + 1)
	if pathBudget < 4 {
		pathBudget = 4
	}
	pathCol := st.TruncateEnd(d.URL(), pathBudget)

	// Layout: " mark " + time + method + path + sep + " " + meta + " "
	fixed := 3 + lipgloss.Width(timeCol) + 1 + lipgloss.Width(method) + 1 +
		lipgloss.Width(pathCol) + 1 + lipgloss.Width(metaCol) + 1
	sep := strings.Repeat(" ", max(0, width-fixed))
	return " " + markCol + " " + timeCol + " " + method + " " + pathCol + sep + " " + metaCol + " "
}

// emptyList renders the no-deliveries state: a short explanation plus a
// copy-pasteable curl that exercises the listener, so the first useful
// interaction is ten seconds away. Every line is truncated to the pane
// width — a wide hint must grow the rows, never the panel.
func emptyList(st style.Styles, s listState, width int) []string {
	hint := st.TruncateEnd(" waiting for webhooks", width)
	if s.filter != "" {
		return []string{hint, "",
			st.Empty.Render(st.TruncateEnd("no deliveries match "+s.filter, width))}
	}
	example := fmt.Sprintf(" curl -X POST %s/test -d '{\"hello\":\"world\"}'", s.listenURL)
	return []string{
		hint,
		"",
		st.Empty.Render(st.TruncateEnd("send a request to the listener and it lands here:", width)),
		st.EmptyCode.Render(st.TruncateEnd(example, width)),
		st.Empty.Render(st.TruncateEnd("press c on a delivery to copy it as cURL", width)),
	}
}
