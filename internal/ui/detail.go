package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hookline/internal/delivery"
	"hookline/internal/style"
)

// Detail pane tabs, in order. Users switch with 1-4 / h / l.
var detailTabs = []string{"body", "headers", "raw", "response"}

// maxRenderBytes caps how much of a body is rendered per frame; larger
// bodies render their head plus a notice. Keeps 5 MB payloads from ever
// freezing the render loop.
const maxRenderBytes = 100 << 10

type detailModel struct {
	tab    int
	scroll int
	// cache avoids re-tokenising rendered content on every frame; keyed by
	// delivery ID + tab + width, exactly the inputs that change the output.
	cacheKey string
	cache    string
	// prettyCache stores the indented form of JSON bodies once (the
	// indent itself is width-independent); bounded by evicting all entries
	// when it grows past prettyCacheMax.
	prettyCache map[string]string
}

// prettyCacheMax bounds the pretty-JSON cache.
const prettyCacheMax = 32

func newDetailModel() detailModel {
	return detailModel{prettyCache: map[string]string{}}
}

// setTab clamps and resets scroll.
func (d *detailModel) setTab(i int) {
	if i < 0 {
		i = 0
	}
	if i >= len(detailTabs) {
		i = len(detailTabs) - 1
	}
	if i != d.tab {
		d.tab = i
		d.scroll = 0
	}
}

// nextTab / prevTab cycle with wraparound.
func (d *detailModel) nextTab(dir int) { d.setTab((d.tab + dir + len(detailTabs)) % len(detailTabs)) }

// Selected returns the delivery the list currently points at, or nil.
func selectedDelivery(items []*delivery.Delivery, l *listModel) *delivery.Delivery {
	idx := l.selectedIndex(items)
	if idx < 0 {
		return nil
	}
	return items[idx]
}

// View renders the detail panel for delivery d (nil renders a hint).
func (d *detailModel) View(st style.Styles, sel *delivery.Delivery, w, h int, focused bool) string {
	if w < 8 || h < 4 {
		return st.Panel.Render("")
	}
	panel := st.Panel
	if focused {
		panel = st.PanelFocus
	}
	inner := w - 2

	var title, body string
	if sel == nil {
		title = " detail"
		body = st.Empty.Render("select a delivery ↑↓")
	} else {
		title = " " + detailTabs[d.tab]
		note := detailNote(st, sel)
		title += strings.Repeat(" ", max(0, inner-lipgloss.Width(title)-lipgloss.Width(note))) + note
		body = d.renderBody(st, sel, inner)
	}
	content := lipgloss.JoinVertical(lipgloss.Left, title, body)

	// content already includes the title row; inside the border there are
	// exactly h-2 rows to fill (or scroll through).
	lines := strings.Split(content, "\n")
	viewH := h - 2
	if viewH < 1 {
		viewH = 1
	}
	if d.scroll > len(lines)-viewH {
		d.scroll = max(0, len(lines)-viewH)
	}
	if d.scroll > 0 {
		lines = lines[min(d.scroll, len(lines)):]
	}
	for len(lines) < viewH {
		lines = append(lines, blankLine(inner))
	}
	if len(lines) > viewH {
		lines = lines[:viewH]
	}
	return panel.Render(strings.Join(lines, "\n"))
}

// detailNote is the right-aligned context on the title row: method, path,
// size — everything needed to know what you're looking at without the list.
func detailNote(st style.Styles, sel *delivery.Delivery) string {
	method := st.Method(sel.Method).Render(sel.Method)
	path := st.TruncateEnd(sel.URL(), 40)
	return st.PanelNote.Render(method + " " + path + " " + humanSize(sel.Size()))
}

// renderBody renders the active tab's content, memoised per (id, tab, width).
func (d *detailModel) renderBody(st style.Styles, sel *delivery.Delivery, width int) string {
	key := fmt.Sprintf("%s|%d|%d", sel.ID, d.tab, width)
	if key == d.cacheKey && d.cache != "" {
		return d.cache
	}
	var content string
	switch d.tab {
	case 0:
		content = d.renderBodyTab(st, sel, width)
	case 1:
		content = renderHeadersTab(st, sel, width)
	case 2:
		content = st.Plain(sel.Body, width)
	default:
		content = renderResponseTab(st, sel, width)
	}
	d.cacheKey = key
	d.cache = content
	return content
}

// renderBodyTab renders the body tab. Large JSON bodies are indented once
// (cached), then only their head is colourised and wrapped — the render
// loop never tokenises megabytes, and cutting the pretty text mid-line is
// harmless because the tokeniser scans to whatever it is given.
func (d *detailModel) renderBodyTab(st style.Styles, sel *delivery.Delivery, width int) string {
	var b strings.Builder
	if sel.Truncated {
		b.WriteString(st.Note.Render("body was truncated when captured") + "\n")
	}
	switch sel.Kind() {
	case delivery.KindEmpty:
		b.WriteString(st.Empty.Render("(empty body)"))
	case delivery.KindJSON:
		if pretty, ok := d.prettyJSON(sel); ok {
			if len(pretty) > maxRenderBytes {
				b.WriteString(st.JSONPretty(pretty[:maxRenderBytes], width))
				b.WriteString("\n" + st.Note.Render(fmt.Sprintf(
					"… showing first %dK of %s — y copies the full body", maxRenderBytes>>10, humanSize(len(sel.Body)))))
			} else {
				b.WriteString(st.JSONPretty(pretty, width))
			}
		} else {
			b.WriteString(st.Plain(sel.Body, width))
		}
	case delivery.KindForm:
		b.WriteString(st.Form(formPairs(sel.Body), width))
	case delivery.KindBinary:
		b.WriteString(st.HexDump(sel.Body, width))
	default:
		b.WriteString(st.Plain(sel.Body, width))
	}
	return b.String()
}

// prettyJSON returns the indented form of a JSON body, computed once per
// delivery. ok=false means the body does not parse.
func (d *detailModel) prettyJSON(sel *delivery.Delivery) (string, bool) {
	if p, ok := d.prettyCache[sel.ID]; ok {
		return p, true
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, sel.Body, "", "  "); err != nil {
		return "", false
	}
	if len(d.prettyCache) >= prettyCacheMax {
		d.prettyCache = map[string]string{}
	}
	p := buf.String()
	d.prettyCache[sel.ID] = p
	return p, true
}

func renderHeadersTab(st style.Styles, sel *delivery.Delivery, width int) string {
	keys := make([]string, 0, len(sel.Headers))
	for k := range sel.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return st.Empty.Render("(no headers)")
	}
	var b strings.Builder
	for _, k := range keys {
		vals := strings.Join(sel.Headers[k], ", ")
		line := st.Key(k) + ": " + vals
		b.WriteString(st.TruncateEnd(line, width))
		b.WriteString("\n")
	}
	return b.String()
}

func renderResponseTab(st style.Styles, sel *delivery.Delivery, width int) string {
	code := st.StatusStyle(sel.ResponseStatus).Render(fmt.Sprintf("%d", sel.ResponseStatus))
	return "hookline replied " + code + st.PanelNote.Render("  (auto-response; configure with --status)") + "\n" +
		st.Empty.Render("(response body is not recorded)")
}
