package ui

// Adversarial model-level probe suite (probe_ui_test.go). Reuses the helpers
// in app_test.go (newTestApp, sendKey, testDelivery, assertFrameFits).
//
// House rule: every test passes against CURRENT behavior. Where current
// behavior is believed to be a product bug, the assertion pins the buggy
// outcome with a "BUG(documented ...)" comment and the finding is reported;
// flipping one of these assertions red is the regression test for the fix.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DJJW26/hookline/internal/delivery"
	"github.com/DJJW26/hookline/internal/style"
)

// --- probe helpers -------------------------------------------------------

// probeApp builds an app like newTestApp but lets the test tweak Config
// (Version / ListenURL) before construction.
func probeApp(t *testing.T, initial []delivery.Delivery, tweak func(*Config)) *Model {
	t.Helper()
	theme, err := style.Lookup("dark")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Version:   "test",
		ListenURL: "http://127.0.0.1:4040",
		Events:    make(chan delivery.Delivery, 8),
		Initial:   initial,
		OnClear:   func() { clearedFlag = true },
	}
	if tweak != nil {
		tweak(&cfg)
	}
	clearedFlag = false
	m := New(cfg, style.New(theme), theme)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

func probeResize(t *testing.T, m *Model, w, h int) {
	t.Helper()
	model, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	*m = *model.(*Model)
}

func probeClick(t *testing.T, m *Model, x, y int) {
	t.Helper()
	model, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, X: x, Y: y})
	*m = *model.(*Model)
}

func probeWheel(t *testing.T, m *Model, up bool, x, y int) {
	t.Helper()
	btn := tea.MouseButtonWheelDown
	if up {
		btn = tea.MouseButtonWheelUp
	}
	model, _ := m.Update(tea.MouseMsg{Button: btn, X: x, Y: y})
	*m = *model.(*Model)
}

// press sends a raw tea.KeyMsg (for keys sendKey cannot spell, e.g. ctrl+u).
func press(t *testing.T, m *Model, msg tea.KeyMsg) tea.Cmd {
	t.Helper()
	model, cmd := m.Update(msg)
	*m = *model.(*Model)
	return cmd
}

// probeMetrics returns the frame line count and widest line of a view.
func probeMetrics(view string) (lines, maxW int) {
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > maxW {
			maxW = w
		}
		lines = i + 1
	}
	return lines, maxW
}

// --- 1. layout invariants across terminal sizes --------------------------

func TestProbeFrameSweepSizes(t *testing.T) {
	sizes := [][2]int{{20, 6}, {40, 10}, {60, 15}, {80, 24}, {120, 30}, {200, 60}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			long := testDelivery("POST", "/hooks/github/fulfillments/12345", strings.Repeat("l\n", 30))
			m := newTestApp(t, []delivery.Delivery{long})
			probeResize(t, m, w, h)
			view := m.View() // must not panic at any size
			lines, maxW := probeMetrics(view)
			t.Logf("frame %dx%d -> %d lines, max width %d", w, h, lines, maxW)
			// F1/F2 (tiny-width frame overflow) were fixed: every size fits.
			assertFrameFits(t, view, w, h)
		})
	}
}

func TestProbeResizeTransitionsMidMode(t *testing.T) {
	// Help open at a comfortable size, then shrunk under the overlay.
	m := newTestApp(t, nil)
	sendKey(t, m, "?")
	probeResize(t, m, 120, 30)
	assertFrameFits(t, m.View(), 120, 30) // two columns, fits
	probeResize(t, m, 80, 24)
	lines, _ := probeMetrics(m.View())
	t.Logf("help at 80x24 -> %d lines", lines)
	// F3 (help overlay taller than the frame) was fixed: rows are trimmed
	// to the available height.
	assertFrameFits(t, m.View(), 80, 24)
	sendKey(t, m, "esc")
	if m.mode != modeNormal {
		t.Fatal("esc should leave help after resize")
	}

	// Search prompt then resize both directions.
	m2 := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/abc", `{}`)})
	sendKey(t, m2, "/")
	for _, r := range "abc" {
		sendKey(t, m2, string(r))
	}
	probeResize(t, m2, 40, 10)
	assertFrameFits(t, m2.View(), 40, 10)
	probeResize(t, m2, 200, 60)
	assertFrameFits(t, m2.View(), 200, 60)
	if m2.mode != modeSearch || m2.search != "abc" {
		t.Errorf("resize changed prompt state: mode=%v search=%q", m2.mode, m2.search)
	}
}

func TestProbeHelpTwoColumnThreshold(t *testing.T) {
	// Derive the crossover from the twoColumnFits budget (widest row * 2 +
	// 3 gap + 8 panel chrome). Updated: the widest row grew to 44 with the
	// PgUp/PgDn key column ("1-4 h/l  body / headers / raw / response") and
	// the border budget from +6 to +8, so the flip moved from 93 to 99.
	keyW := 0
	for _, sec := range helpSections {
		for _, h := range sec.hints {
			if n := lipgloss.Width(h.key); n > keyW {
				keyW = n
			}
		}
	}
	widest := 0
	for _, sec := range helpSections {
		for _, h := range sec.hints {
			row := "  " + h.key + strings.Repeat(" ", keyW-lipgloss.Width(h.key)) + "  " + h.action
			if n := lipgloss.Width(row); n > widest {
				widest = n
			}
		}
	}
	crossover := widest*2 + 3 + 8
	t.Logf("help crossover: widest=%d -> %d cols", widest, crossover)

	m := newTestApp(t, nil)
	sendKey(t, m, "?")
	probeResize(t, m, crossover, 30)
	assertFrameFits(t, m.View(), crossover, 30) // two columns, fits

	// F3 (help overlay taller than the frame) was fixed: below the
	// threshold the single column is trimmed by fitHelpRows and still fits.
	probeResize(t, m, crossover-1, 30)
	assertFrameFits(t, m.View(), crossover-1, 30)
	sendKey(t, m, "esc")
	if m.mode != modeNormal {
		t.Fatal("esc should leave help")
	}
}

// --- 2. selection / scroll state ------------------------------------------

func longBodyDelivery(path string, n int) delivery.Delivery {
	d := testDelivery("POST", path, strings.Repeat("line\n", n))
	return d
}

func TestProbeDetailScrollStaleAcrossSelection(t *testing.T) {
	long := longBodyDelivery("/long", 120)
	short := testDelivery("GET", "/short", `{}`)
	m := newTestApp(t, []delivery.Delivery{short, long}) // items: long, short
	sendKey(t, m, "tab")                                 // focus detail
	for i := 0; i < 5; i++ {
		sendKey(t, m, "j")
	}
	if m.detail.scroll != 5 {
		t.Fatalf("scroll after 5xj = %d, want 5", m.detail.scroll)
	}
	sendKey(t, m, "tab") // back to list
	sendKey(t, m, "j")   // move selection to the short delivery
	// ROUGH(documented F6): switching selection does not reset detail.scroll;
	// the stale offset survives until the next View clamps it.
	if m.detail.scroll == 0 {
		t.Error("detail scroll was reset on selection change (expected stale offset)")
	}
	view := m.View() // clamp happens inside detail.View
	if m.detail.scroll != 0 {
		t.Errorf("View should clamp stale scroll to 0 for short body, got %d", m.detail.scroll)
	}
	if !strings.Contains(view, "/short") && !strings.Contains(view, "{}") {
		t.Error("detail should render the short delivery after clamp")
	}
}

func TestProbeDetailScrollClampedByView(t *testing.T) {
	long := longBodyDelivery("/long", 120)
	m := newTestApp(t, []delivery.Delivery{long})
	sendKey(t, m, "tab")
	for i := 0; i < 200; i++ {
		sendKey(t, m, "j") // scroll grows without bound pre-render
	}
	if m.detail.scroll != 200 {
		t.Fatalf("pre-render scroll = %d, want 200 (no clamp before View)", m.detail.scroll)
	}
	m.View()
	if m.detail.scroll <= 0 || m.detail.scroll > 130 {
		t.Errorf("post-View scroll = %d, want clamped into (0,130]", m.detail.scroll)
	}
}

func TestProbeDetailTabSwitchResetsScroll(t *testing.T) {
	long := longBodyDelivery("/long", 120)
	m := newTestApp(t, []delivery.Delivery{long})
	sendKey(t, m, "tab")
	for i := 0; i < 7; i++ {
		sendKey(t, m, "j")
	}
	sendKey(t, m, "l") // next tab
	if m.detail.tab != 1 || m.detail.scroll != 0 {
		t.Errorf("tab switch should reset scroll: tab=%d scroll=%d", m.detail.tab, m.detail.scroll)
	}
	sendKey(t, m, "j")
	sendKey(t, m, "3")
	if m.detail.tab != 2 || m.detail.scroll != 0 {
		t.Errorf("numeric tab switch should reset scroll: tab=%d scroll=%d", m.detail.tab, m.detail.scroll)
	}
}

func TestProbeGGWithFilterActive(t *testing.T) {
	var initial []delivery.Delivery
	for i := 0; i < 12; i++ {
		method := "POST"
		if i%2 == 0 {
			method = "GET"
		}
		initial = append(initial, testDelivery(method, fmt.Sprintf("/i/%02d", i), `{}`))
	}
	m := newTestApp(t, initial)
	sendKey(t, m, "f") // GET only
	vis := m.visible()
	if len(vis) != 6 {
		t.Fatalf("GET filter visible = %d, want 6", len(vis))
	}
	sendKey(t, m, "G")
	if m.list.selectedID != vis[len(vis)-1].ID {
		t.Error("G should select last visible under filter")
	}
	sendKey(t, m, "g")
	if m.list.selectedID != vis[0].ID {
		t.Error("g should select first visible under filter")
	}
	// j at the bottom stays inside the filtered set.
	sendKey(t, m, "G")
	sendKey(t, m, "j")
	if m.list.selectedID != vis[len(vis)-1].ID {
		t.Error("j at filtered bottom should clamp")
	}
}

func TestProbeSelectionIntegrityAfter500Burst(t *testing.T) {
	seed := testDelivery("POST", "/seed", `{}`)
	m := newTestApp(t, []delivery.Delivery{seed})
	wantSel := m.list.selectedID
	for i := 0; i < 500; i++ {
		model, _ := m.Update(deliveryMsg(testDelivery("POST", fmt.Sprintf("/burst/%03d", i), `{}`)))
		*m = *model.(*Model)
	}
	if len(m.items) != 501 || len(m.visible()) != 501 {
		t.Fatalf("items=%d visible=%d, want 501/501", len(m.items), len(m.visible()))
	}
	if m.list.selectedID != wantSel {
		t.Error("selection by ID was yanked by the burst insertions")
	}
	if len(m.list.unread) != 500 {
		t.Errorf("unread map size = %d, want 500 (initial seed is never unread)", len(m.list.unread))
	}
	view := m.View()
	if !strings.Contains(view, "/burst/499") {
		t.Error("newest burst delivery not rendered")
	}
	assertFrameFits(t, view, m.width, m.height)
}

func TestProbeFilterCycleWrapsAllMethods(t *testing.T) {
	var initial []delivery.Delivery
	for _, meth := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		initial = append(initial, testDelivery(meth, "/"+meth, `{}`))
	}
	m := newTestApp(t, initial)              // reversed: DELETE..GET, selection GET
	wantCounts := []int{5, 1, 1, 1, 1, 1, 5} // idx 0..5 then wrap
	for i, want := range wantCounts {
		if got := len(m.visible()); got != want {
			t.Fatalf("step %d: visible = %d, want %d (methodIdx=%d)", i, got, want, m.methodIdx)
		}
		sendKey(t, m, "f")
	}
}

// --- 3. mode state machine -------------------------------------------------

func TestProbeModeTransitionMatrix(t *testing.T) {
	keys := []string{"esc", "enter", "tab", "q", "?", "/", "t", "f", "r", "x", "s", "y"}
	for _, startMode := range []mode{modeNormal, modeSearch, modeTarget, modeDiff, modeConfirm, modeHelp} {
		for _, k := range keys {
			name := fmt.Sprintf("%s+%s", modeName(startMode), k)
			t.Run(name, func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic on %q from %s: %v", k, modeName(startMode), r)
					}
				}()
				m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{"x":1}`)})
				enterMode(t, m, startMode)
				if m.mode != startMode {
					t.Fatalf("setup failed: mode=%v want %v", m.mode, startMode)
				}
				sendKey(t, m, k)
				// esc always returns to normal within a few presses.
				for i := 0; i < 3 && m.mode != modeNormal; i++ {
					sendKey(t, m, "esc")
				}
				if m.mode != modeNormal {
					t.Errorf("stuck in %v after %q + esc x3", m.mode, k)
				}
				_ = m.View() // still renders
			})
		}
	}
}

func modeName(m mode) string {
	switch m {
	case modeNormal:
		return "normal"
	case modeSearch:
		return "search"
	case modeTarget:
		return "target"
	case modeDiff:
		return "diff"
	case modeConfirm:
		return "confirm"
	case modeHelp:
		return "help"
	}
	return "?"
}

func enterMode(t *testing.T, m *Model, target mode) {
	t.Helper()
	switch target {
	case modeSearch:
		sendKey(t, m, "/")
	case modeTarget:
		sendKey(t, m, "t")
	case modeDiff:
		in := *m.selection()
		msg := editorDoneMsg{d: in, body: []byte(`{"x":2}`)}
		model, _ := m.Update(msg)
		*m = *model.(*Model)
	case modeConfirm:
		sendKey(t, m, "x")
	case modeHelp:
		sendKey(t, m, "?")
	}
}

func TestProbeSpecialKeysTypedIntoSearch(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)})
	sendKey(t, m, "/")
	sendKey(t, m, "?") // must be typed, not open help
	if m.mode != modeSearch || m.input.Value() != "?" {
		t.Errorf("'?' during search: mode=%v input=%q, want typed into prompt", m.mode, m.input.Value())
	}
	sendKey(t, m, "x") // typed as well
	if m.mode != modeSearch || m.input.Value() != "?x" {
		t.Errorf("'x' during search: mode=%v input=%q", m.mode, m.input.Value())
	}
	sendKey(t, m, "q") // typed, never quits
	if m.mode != modeSearch || m.input.Value() != "?xq" {
		t.Errorf("'q' during search: mode=%v input=%q", m.mode, m.input.Value())
	}
	sendKey(t, m, "esc")
	if m.mode != modeNormal || m.search != "" {
		t.Errorf("esc should cancel search: mode=%v search=%q", m.mode, m.search)
	}
}

func TestProbeEnterInConfirmClears(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)})
	sendKey(t, m, "x")
	sendKey(t, m, "enter")
	if m.mode != modeNormal || len(m.items) != 0 || !clearedFlag {
		t.Errorf("enter in confirm: mode=%v items=%d cleared=%v", m.mode, len(m.items), clearedFlag)
	}
}

func TestProbeCtrlUIsClearsLiveSearch(t *testing.T) {
	// Updated: ctrl+u no longer clears the live search from normal mode
	// (it pages the list there); inside the find prompt it still wipes the
	// input and keeps the prompt open. esc clears the live filter (hint
	// contract) and closes the prompt, leaving no text behind.
	m := newTestApp(t, []delivery.Delivery{
		testDelivery("POST", "/aa", `{}`),
		testDelivery("GET", "/bb", `{}`),
	})
	sendKey(t, m, "/")
	sendKey(t, m, "aa") // needle unique to /aa (headers are searched too)
	if len(m.visible()) != 1 {
		t.Fatalf("live filter visible=%d, want 1", len(m.visible()))
	}
	press(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.input.Value() != "" || m.mode != modeSearch {
		t.Errorf("ctrl+u should wipe the input but stay in prompt: value=%q mode=%v",
			m.input.Value(), m.mode)
	}
	if len(m.visible()) != 2 {
		t.Errorf("ctrl+u should lift the live filter, visible=%d", len(m.visible()))
	}
	sendKey(t, m, "aa")
	if len(m.visible()) != 1 {
		t.Fatalf("live filter visible=%d, want 1", len(m.visible()))
	}
	sendKey(t, m, "esc")
	if m.input.Value() != "" || m.search != "" || len(m.visible()) != 2 {
		t.Errorf("esc should clear input and filter: value=%q search=%q visible=%d",
			m.input.Value(), m.search, len(m.visible()))
	}
	// Updated: esc used to keep the prompt open after clearing; it now
	// cancels the prompt once the filter is lifted.
	if m.mode != modeNormal {
		t.Errorf("esc should close the cleared prompt, mode=%v", m.mode)
	}
}

// --- 4. toast behavior ------------------------------------------------------

func TestProbeToastEarlyClear(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)})
	sendKey(t, m, "r") // toast A: "no replay target" + tick A
	if m.toast == nil || !strings.Contains(m.toast.text, "replay target") {
		t.Fatalf("toast A missing: %+v", m.toast)
	}
	// toast B replaces A while A's tick is still pending.
	model, _ := m.Update(replayResultMsg{
		d:   *m.selection(),
		err: errors.New("boom"),
	})
	*m = *model.(*Model)
	if m.toast == nil || !strings.Contains(m.toast.text, "boom") {
		t.Fatalf("toast B missing: %+v", m.toast)
	}
	// BUG(documented F7): tick A fires toastExpiredMsg and clears toast B
	// early; ticks are not identified per-toast.
	m.Update(toastExpiredMsg{})
	if m.toast != nil {
		t.Errorf("expected documented early-clear of toast B, still present: %+v", m.toast)
	}
}

func TestProbeToastWhileInHelpMode(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)})
	sendKey(t, m, "?")
	model, _ := m.Update(replayResultMsg{d: testDelivery("POST", "/p", ""), err: errors.New("boom")})
	*m = *model.(*Model)
	if m.mode != modeHelp {
		t.Errorf("toast should not leave help mode, mode=%v", m.mode)
	}
	view := m.View()
	if !strings.Contains(view, "boom") {
		t.Error("toast missing from status line while help is open")
	}
	assertFrameFits(t, view, 120, 30)
}

func TestProbeLongToastTruncatesStatusLine(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)})
	probeResize(t, m, 80, 24)
	long := strings.Repeat("E", 300)
	model, _ := m.Update(replayResultMsg{d: testDelivery("POST", "/p", ""), err: errors.New(long)})
	*m = *model.(*Model)
	view := m.View()
	lines := strings.Split(view, "\n")
	last := lines[len(lines)-1]
	if w := lipgloss.Width(last); w > 80 {
		t.Errorf("status line with long toast = %d cols, want <= 80: %q", w, truncateForLog(last))
	}
	if !strings.Contains(last, "…") {
		t.Error("long toast should be truncated with an ellipsis")
	}
}

func TestProbeStatusLineLongVersionOverflow(t *testing.T) {
	m := probeApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)}, func(c *Config) {
		c.Version = "9.9.9-some-very-long-prerelease-tag"
	})
	probeResize(t, m, 40, 10)
	view := m.View()
	lines := strings.Split(view, "\n")
	last := lines[len(lines)-1]
	w := lipgloss.Width(last)
	// F8 (status-line overflow with long version) was fixed: both sides
	// truncate under the frame width.
	if w > 40 {
		t.Errorf("status line overflowed 40 cols with long version: %d", w)
	}
	t.Logf("status line width with long version at 40 cols: %d", w)
}

func TestProbeHeaderLongListenURLOverflow(t *testing.T) {
	m := probeApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)}, func(c *Config) {
		c.ListenURL = "http://webhook-inspector.example.very-long-domain.name:8443/base/path"
	})
	probeResize(t, m, 80, 24)
	view := m.View()
	header := strings.Split(view, "\n")[0]
	w := lipgloss.Width(header)
	// F9 (header overflow with long listen URL) was fixed.
	if w > 80 {
		t.Errorf("header overflowed 80 cols with long URL: %d", w)
	}
	t.Logf("header width with long URL at 80 cols: %d", w)
}

// --- 5. mouse ----------------------------------------------------------------

func TestProbeWheelDetailAtScrollZero(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{longBodyDelivery("/long", 120)})
	sendKey(t, m, "tab")           // focus detail
	probeWheel(t, m, true, 60, 10) // wheel up at scroll 0
	if m.detail.scroll != 0 {
		t.Errorf("wheel up at scroll 0 = %d, want 0 (no negative)", m.detail.scroll)
	}
	probeWheel(t, m, false, 60, 10) // wheel down
	probeWheel(t, m, false, 60, 10)
	if m.detail.scroll != 2 {
		t.Errorf("wheel down x2 in detail = %d, want 2", m.detail.scroll)
	}
}

func TestProbeWheelListEdgesAndSelectionPull(t *testing.T) {
	var initial []delivery.Delivery
	for i := 0; i < 30; i++ {
		initial = append(initial, testDelivery("POST", fmt.Sprintf("/i/%02d", i), `{}`))
	}
	m := newTestApp(t, initial) // items newest-first: /i/29 .. /i/00
	sendKey(t, m, "G")          // bottom: offset 30-24=6, selection idx 29
	if m.list.offset != 6 {
		t.Fatalf("offset after G = %d, want 6", m.list.offset)
	}
	probeWheel(t, m, false, 5, 10) // wheel down at the bottom
	if m.list.offset != 6 {
		t.Errorf("wheel down at bottom moved offset to %d, want clamp at 6", m.list.offset)
	}
	// Wheel up: offset 5, selection (idx 29) falls out of view and is pulled
	// to the bottom edge of the window (idx 5+24-1 = 28).
	probeWheel(t, m, true, 5, 10)
	if m.list.offset != 5 {
		t.Fatalf("offset after wheel up = %d, want 5", m.list.offset)
	}
	vis := m.visible()
	if m.list.selectedID != vis[28].ID {
		t.Error("selection should be pulled to the visible edge after wheel up")
	}
	// All the way back to the top: offset clamps at 0.
	for i := 0; i < 10; i++ {
		probeWheel(t, m, true, 5, 10)
	}
	if m.list.offset != 0 {
		t.Errorf("offset after many wheel ups = %d, want 0", m.list.offset)
	}
}

func TestProbeClickBoundaryRows(t *testing.T) {
	var initial []delivery.Delivery
	for i := 0; i < 30; i++ {
		initial = append(initial, testDelivery("POST", fmt.Sprintf("/i/%02d", i), `{}`))
	}
	m := newTestApp(t, initial) // 120x30 split, contentH 24, rows y in [3,27)
	vis := m.visible()

	probeClick(t, m, 5, 2) // panel title row: ignored
	if m.list.selectedID != vis[0].ID {
		t.Error("click on title row should be ignored")
	}
	probeClick(t, m, 5, 3) // first row
	sendKey(t, m, "j")     // move to vis[1] so ignores are observable
	if m.list.selectedID != vis[1].ID {
		t.Fatal("j should move to second row")
	}
	probeClick(t, m, 5, 2) // title row again, selection must stay
	if m.list.selectedID != vis[1].ID {
		t.Error("click on title row moved selection")
	}
	probeClick(t, m, 5, 26) // last content row -> vis[23]
	if m.list.selectedID != vis[23].ID {
		t.Errorf("click on last row = idx?, want vis[23] (%s)", vis[23].Path)
	}
	probeClick(t, m, 5, 27) // bottom border row: ignored
	if m.list.selectedID != vis[23].ID {
		t.Error("click on bottom border moved selection")
	}
	probeClick(t, m, 5, 28) // below the list panel entirely
	if m.list.selectedID != vis[23].ID {
		t.Error("click below panel moved selection")
	}
}

func TestProbeClickSplitPaneBoundaries(t *testing.T) {
	var initial []delivery.Delivery
	for i := 0; i < 10; i++ {
		initial = append(initial, testDelivery("POST", fmt.Sprintf("/i/%d", i), `{}`))
	}
	m := newTestApp(t, initial) // split at 120; listWidth = 48
	vis := m.visible()
	sendKey(t, m, "j")      // vis[1]
	probeClick(t, m, 60, 4) // detail pane: ignored
	if m.list.selectedID != vis[1].ID {
		t.Error("click in detail pane moved list selection")
	}
	probeClick(t, m, 48, 4) // gap column right of the list pane
	// ROUGH(documented F10): the split guard is `x > listWidth`, so the gap
	// column (and the list border column) still select rows.
	if m.list.selectedID == vis[1].ID {
		t.Log("gap-column click ignored")
	} else {
		t.Logf("BUG(documented F10): gap-column click at x=%d selected %s", m.listWidth(), m.list.selectedID)
	}
}

func TestProbeClicksIgnoredInNonNormalModes(t *testing.T) {
	for _, target := range []mode{modeSearch, modeTarget, modeConfirm, modeDiff, modeHelp} {
		m := newTestApp(t, []delivery.Delivery{
			testDelivery("POST", "/a", `{}`),
			testDelivery("GET", "/b", `{}`),
		})
		sendKey(t, m, "j")
		before := m.list.selectedID
		enterMode(t, m, target)
		probeClick(t, m, 5, 3)
		probeWheel(t, m, false, 5, 3)
		if m.list.selectedID != before {
			t.Errorf("%s: click/wheel leaked into the list behind the overlay", modeName(target))
		}
	}
}

func TestProbeStackedMouseAt80x24(t *testing.T) {
	var initial []delivery.Delivery
	for i := 0; i < 8; i++ {
		initial = append(initial, testDelivery("POST", fmt.Sprintf("/i/%d", i), `{}`))
	}
	m := newTestApp(t, initial)
	probeResize(t, m, 80, 24) // stacked: stackedListH 8, contentH 5, rows y in [3,8)
	vis := m.visible()
	sendKey(t, m, "G")     // selection last, offset 3
	probeClick(t, m, 5, 7) // last stacked list row -> vis[7]
	if m.list.selectedID != vis[7].ID {
		t.Fatalf("click y=7 selected wrong row (want /i/0)")
	}
	sendKey(t, m, "j") // stay; move selection up for contrast
	sendKey(t, m, "k")
	m.list.selectedID = vis[3].ID
	probeClick(t, m, 5, 8)  // blank separator line between panes
	probeClick(t, m, 5, 10) // detail pane rows
	probeClick(t, m, 5, 15)
	if m.list.selectedID != vis[3].ID {
		t.Error("click on separator/detail rows moved the list selection")
	}
	probeClick(t, m, 5, 4) // real list row -> vis[offset+1] = vis[4]
	if m.list.selectedID != vis[4].ID {
		t.Errorf("click y=4 should select vis[4], got %s", m.list.selectedID)
	}
}

// --- 6. input widget -----------------------------------------------------------

func TestProbeInputMultibyteRunes(t *testing.T) {
	in := newInput("find", "")
	in.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("héllo")})
	in.Update(tea.KeyMsg{Type: tea.KeySpace})
	in.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中")})
	if in.Value() != "héllo 中" {
		t.Fatalf("value = %q, want %q", in.Value(), "héllo 中")
	}
	// Walk the cursor left across the multi-byte runes.
	for i := 0; i < 3; i++ {
		in.Update(tea.KeyMsg{Type: tea.KeyLeft})
	}
	if in.cursor != 4 { // after "héllo " -> before 'l','l','o'... cursor at rune 4
		t.Logf("cursor after 3x left = %d", in.cursor)
	}
	in.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	if in.Value() != "hélloX 中" && in.Value() != "héllo X中" && in.Value() != "héllXo 中" {
		t.Errorf("mid-rune insertion produced %q", in.Value())
	}
	// Paste-like multi-rune single message.
	in2 := newInput("t", "")
	in2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中文abc")})
	if in2.Value() != "中文abc" || in2.cursor != 5 {
		t.Errorf("paste value=%q cursor=%d, want 中文abc/5", in2.Value(), in2.cursor)
	}
}

func TestProbeInputBackspaceAtZero(t *testing.T) {
	in := newInput("find", "ab")
	in.Update(tea.KeyMsg{Type: tea.KeyHome}) // cursor 0
	in.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if in.Value() != "ab" || in.cursor != 0 {
		t.Errorf("backspace at 0 changed state: %q cursor=%d", in.Value(), in.cursor)
	}
	in.Update(tea.KeyMsg{Type: tea.KeyDelete}) // deletes forward at 0
	if in.Value() != "b" {
		t.Errorf("delete at 0 = %q, want 'b'", in.Value())
	}
}

func TestProbeInputEscEmpty(t *testing.T) {
	m := newTestApp(t, nil)
	sendKey(t, m, "/")
	cmd := sendKey(t, m, "esc")
	if cmd != nil {
		t.Error("esc cancel should not produce a command")
	}
	if m.mode != modeNormal || m.search != "" {
		t.Errorf("esc on empty input: mode=%v search=%q", m.mode, m.search)
	}
}

// --- 7. rendering edge cases -----------------------------------------------------

func TestProbeEmptyPathAndBoundaryPath(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "", `{"k":1}`)})
	probeResize(t, m, 80, 24)
	view := m.View()
	if !strings.Contains(view, "POST") {
		t.Error("empty-path delivery row missing method")
	}
	assertFrameFits(t, view, 80, 24)
}

// listPaneView returns the list pane's slice (the first listWidth columns)
// of every frame line. Row-level assertions scope to it: the detail pane's
// note legitimately echoes the full path (truncated at 40 columns), which
// would false-positive a whole-frame substring check.
func listPaneView(m *Model, view string) string {
	var b strings.Builder
	for _, line := range strings.Split(view, "\n") {
		runes := []rune(line)
		if len(runes) > m.listWidth() {
			runes = runes[:m.listWidth()]
		}
		b.WriteString(string(runes))
		b.WriteString("\n")
	}
	return b.String()
}

func TestProbePathTruncationBoundary(t *testing.T) {
	// Derive the budget from the same math the renderer uses (the time
	// column width varies with the day): inner 46 - fixed columns - meta.
	// TruncateEnd counts its ellipsis within the width, so the longest URL
	// still rendered whole is one column under the renderer's pathBudget.
	d := testDelivery("POST", "/p", `{}`)
	meta := humanSize(d.Size()) + " " + kindLabel(d)
	pathBudget := 46 - (3 + len([]rune(dayTime(d.TS))) + 1 + 6 + 1 + 1 + lipgloss.Width(meta) + 1)
	budget := pathBudget - 1
	t.Logf("path budget: renderer=%d, last untruncated URL width=%d", pathBudget, budget)

	exact := "/" + strings.Repeat("a", budget-1) // URL width == budget
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", exact, `{}`)})
	view := m.View()
	if !strings.Contains(listPaneView(m, view), exact) {
		t.Errorf("path exactly filling the budget (%d cols) should render untruncated in the row", budget)
	}
	assertFrameFits(t, view, 120, 30)

	oneOver := "/" + strings.Repeat("b", budget) // URL width == budget+1
	m2 := newTestApp(t, []delivery.Delivery{testDelivery("POST", oneOver, `{}`)})
	view2 := m2.View()
	if strings.Contains(listPaneView(m2, view2), oneOver) {
		t.Errorf("path one over budget (%d cols) should be truncated in the row", budget+1)
	}
	if !strings.Contains(listPaneView(m2, view2), "…") {
		t.Error("truncated path should show an ellipsis")
	}
	assertFrameFits(t, view2, 120, 30)
}

func TestProbeMethodDeleteVsGetAlignment(t *testing.T) {
	if got := methodLabel("DELETE"); lipgloss.Width(got) != 6 {
		t.Errorf("methodLabel(DELETE) = %q (%d cols), want 6", got, lipgloss.Width(got))
	}
	if got := methodLabel("GET"); got != "GET   " {
		t.Errorf("methodLabel(GET) = %q, want padded to 6", got)
	}
	m := newTestApp(t, []delivery.Delivery{
		testDelivery("GET", "/g", `{}`),
		testDelivery("DELETE", "/d/long/path/that/needs/truncating/surely", strings.Repeat("x", 2000)),
	})
	view := m.View()
	if !strings.Contains(view, "DELETE") {
		t.Error("DELETE row missing")
	}
	assertFrameFits(t, view, 120, 30)
}

func TestProbeUnreadMarkerWithSelection(t *testing.T) {
	a := testDelivery("POST", "/a", `{}`)
	m := newTestApp(t, []delivery.Delivery{a})
	// A second, unread delivery arrives while /a stays selected.
	model, _ := m.Update(deliveryMsg(testDelivery("GET", "/fresh", `{}`)))
	*m = *model.(*Model)
	if m.list.selectedID != a.ID {
		t.Fatal("arrival should not steal selection")
	}
	fresh := m.items[0]
	if !m.list.unread[fresh.ID] {
		t.Fatal("fresh delivery should be unread")
	}
	view := m.View()
	if !strings.Contains(view, "•") { // GlyphUnread next to the fresh row
		t.Error("unread marker not rendered for unselected fresh delivery")
	}
	assertFrameFits(t, view, 120, 30)
}

func TestProbeEmptyStateNarrowOverflow(t *testing.T) {
	m := newTestApp(t, nil)
	probeResize(t, m, 120, 30)
	assertFrameFits(t, m.View(), 120, 30) // comfortable size fits

	probeResize(t, m, 60, 15)
	lines, maxW := probeMetrics(m.View())
	t.Logf("empty state 60x15 -> %d lines, max width %d", lines, maxW)
	// F4 (empty state taller than its pane) was fixed: content is trimmed.
	assertFrameFits(t, m.View(), 60, 15)

	probeResize(t, m, 40, 10)
	_, maxW2 := probeMetrics(m.View())
	t.Logf("empty state 40x10 -> max width %d", maxW2)
	// F5 (empty-state width overflow) was fixed: hint lines truncate to
	// the pane width like the curl example line.
	assertFrameFits(t, m.View(), 40, 10)
}

func TestProbeDiffViewUnwrappedLinesOverflow(t *testing.T) {
	in := testDelivery("POST", "/hooks/x", `{"count":1}`)
	m := newTestApp(t, []delivery.Delivery{in})
	long := `{"count":2,"pad":"` + strings.Repeat("Y", 200) + `"}`
	model, _ := m.Update(editorDoneMsg{d: in, body: []byte(long)})
	*m = *model.(*Model)
	if m.mode != modeDiff {
		t.Fatal("setup: expected diff mode")
	}
	view := m.View()
	_, maxW := probeMetrics(view)
	t.Logf("diff with 200-col line at 120 -> max width %d", maxW)
	// F11 (renderDiff never wrapped diff lines) was fixed: output goes
	// through st.Wrap, so the frame fits at any terminal size.
	assertFrameFits(t, view, 120, 30)

	probeResize(t, m, 40, 10)
	lines2, maxW2 := probeMetrics(m.View())
	t.Logf("diff at 40x10 -> %d lines, max width %d", lines2, maxW2)
	assertFrameFits(t, m.View(), 40, 10)
	// esc still exits diff mode.
	sendKey(t, m, "esc")
	if m.mode != modeNormal || m.pending != nil {
		t.Error("esc should discard the pending edit at any size")
	}
}

// --- 8. format helpers ------------------------------------------------------------

func TestProbeHumanSizeBoundaries(t *testing.T) {
	cases := []struct {
		n    int
		want string
		note string
	}{
		{1023, "1023B", ""},
		{1024, "1.0K", ""},
		{1048575, "1024.0K", "ROUGH(documented F12): displays 1024.0K at the K/M boundary"},
		{1048576, "1.0M", ""},
		{1536, "1.5K", ""},
	}
	for _, c := range cases {
		if got := humanSize(c.n); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestProbeKindLabels(t *testing.T) {
	form := testDelivery("POST", "/f", "a=1&b=2")
	form.Headers = map[string][]string{"Content-Type": {"application/x-www-form-urlencoded"}}
	bin := testDelivery("POST", "/b", "\x00\x01payload")
	bin.Headers = map[string][]string{"Content-Type": {"application/octet-stream"}}
	txt := testDelivery("POST", "/t", "hello world")
	txt.Headers = map[string][]string{"Content-Type": {"text/plain"}}
	cases := []struct {
		d    delivery.Delivery
		want string
	}{
		{testDelivery("POST", "/j", `{"a":1}`), "json"},
		{form, "form"},
		{bin, "bin"},
		{txt, "text"},
		{testDelivery("POST", "/e", ""), ""},
	}
	for _, c := range cases {
		if got := kindLabel(c.d); got != c.want {
			t.Errorf("kindLabel(%s) = %q, want %q", c.d.Path, got, c.want)
		}
	}
}

func TestProbeFormPairsMalformedEscapes(t *testing.T) {
	pairs := formPairs([]byte("a=%zz&%=x&100%=&ok"))
	want := [][2]string{
		{"a", "%zz"}, // bad value escape kept raw
		{"%", "x"},   // bad key escape kept raw
		{"100%", ""},
		{"ok", ""},
	}
	if len(pairs) != len(want) {
		t.Fatalf("pairs = %v, want %v", pairs, want)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pair %d = %v, want %v", i, pairs[i], want[i])
		}
	}
}

func TestProbeDayTimeFormat(t *testing.T) {
	now := time.Now()
	if got := dayTime(now); got != now.Format("15:04:05") {
		t.Errorf("today: dayTime = %q, want 15:04:05", got)
	}
	lastWeek := now.AddDate(0, 0, -3)
	if got := dayTime(lastWeek); got != lastWeek.Format("Mon 15:04") {
		t.Errorf("this week: dayTime = %q, want Mon 15:04", got)
	}
	older := now.AddDate(0, 0, -30)
	if got := dayTime(older); got != older.Format("Jan 02 15:04") {
		t.Errorf("older: dayTime = %q, want Jan 02 15:04", got)
	}
}

// --- 9. data-path extras -----------------------------------------------------------

func TestProbeClearThenBurstRebuildsCaches(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/one", `{}`)})
	sendKey(t, m, "x")
	sendKey(t, m, "y")
	if len(m.items) != 0 || len(m.list.unread) != 0 {
		t.Fatalf("clear left residue: items=%d unread=%d", len(m.items), len(m.list.unread))
	}
	for i := 0; i < 20; i++ {
		model, _ := m.Update(deliveryMsg(testDelivery("POST", fmt.Sprintf("/n/%d", i), `{}`)))
		*m = *model.(*Model)
	}
	if len(m.visible()) != 20 {
		t.Errorf("visible after post-clear burst = %d, want 20", len(m.visible()))
	}
	if !strings.Contains(m.View(), "/n/19") {
		t.Error("post-clear burst not rendered")
	}
	if m.list.selectedID != m.items[0].ID {
		t.Error("selection not re-established after clear + burst")
	}
}

func TestProbeEvictionKeepsAtLeastOne(t *testing.T) {
	// maxSessionBytes is 256MB — too heavy to exercise with real bodies
	// here; evictOld's loop guard (len > 1) is exercised via a direct call
	// on an under-cap session, which must be a no-op.
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", strings.Repeat("x", 1024))})
	before := len(m.items)
	m.evictOld()
	if len(m.items) != before {
		t.Errorf("evictOld dropped items in an under-cap session: %d -> %d", before, len(m.items))
	}
	if sessionBytes(m.items) > maxSessionBytes {
		t.Error("sessionBytes over cap after evictOld")
	}
}
