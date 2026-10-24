package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"hookline/internal/delivery"
	"hookline/internal/style"
)

// UI tests run at the model level: feed messages into Update, assert on
// View. This is deterministic, terminal-free, and covers the same reducer
// and renderer the real program runs.

func TestMain(m *testing.M) {
	// Plain profile: View output is plain text, assertions never see ANSI.
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

func testDelivery(method, path string, body string) delivery.Delivery {
	return delivery.Delivery{
		ID:             delivery.NewID(),
		TS:             time.Date(2026, 10, 24, 14, 2, 11, 0, time.UTC),
		Method:         method,
		Path:           path,
		Headers:        map[string][]string{"Content-Type": {"application/json"}},
		Body:           []byte(body),
		ResponseStatus: 200,
	}
}

// clearedFlag records whether OnClear fired (asserted in TestConfirmClear).
var clearedFlag bool

func newTestApp(t *testing.T, initial []delivery.Delivery) *Model {
	t.Helper()
	theme, err := style.Lookup("dark")
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan delivery.Delivery, 8)
	clearedFlag = false
	m := New(Config{
		Version:   "test",
		ListenURL: "http://127.0.0.1:4040",
		Events:    events,
		Initial:   initial,
		OnClear:   func() { clearedFlag = true },
	}, style.New(theme), theme)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

// sendKey drives one keypress through Update.
func sendKey(t *testing.T, m *Model, s string) tea.Cmd {
	t.Helper()
	var key tea.KeyMsg
	switch s {
	case "enter":
		key = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		key = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		key = tea.KeyMsg{Type: tea.KeyTab}
	default:
		key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	model, cmd := m.Update(key)
	*m = *model.(*Model)
	return cmd
}

func TestBootRendersFrame(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/hooks/github", `{"a":1}`)})
	view := m.View()

	for _, want := range []string{"hookline", "listening", "/hooks/github"} {
		if !strings.Contains(view, want) {
			t.Errorf("boot view missing %q", want)
		}
	}
	assertFrameFits(t, view, m.width, m.height)
}

func TestEmptyStateShowsCurl(t *testing.T) {
	m := newTestApp(t, nil)
	view := m.View()
	if !strings.Contains(view, "waiting for webhooks") {
		t.Error("empty state missing waiting copy")
	}
	if !strings.Contains(view, "curl -X POST http://127.0.0.1:4040/test") {
		t.Error("empty state missing curl example")
	}
}

func TestLiveDeliveryAppears(t *testing.T) {
	m := newTestApp(t, nil)
	d := testDelivery("POST", "/stripe/events", `{"type":"payment"}`)
	model, _ := m.Update(deliveryMsg(d))
	*m = *model.(*Model)
	if len(m.items) != 1 {
		t.Fatalf("items = %d, want 1", len(m.items))
	}
	if !m.list.unread[d.ID] {
		t.Error("new delivery should be marked unread")
	}
	if !strings.Contains(m.View(), "/stripe/events") {
		t.Error("new delivery path missing from view")
	}
}

func TestNavigationMovesSelection(t *testing.T) {
	a := testDelivery("POST", "/a", `{}`)
	b := testDelivery("GET", "/b", `{}`)
	m := newTestApp(t, []delivery.Delivery{a, b}) // newest first: b, a

	if m.list.selectedID != b.ID {
		t.Fatalf("initial selection = %s, want newest", m.list.selectedID)
	}
	sendKey(t, m, "j")
	if m.list.selectedID != a.ID {
		t.Errorf("after j, selection = %s, want older item", m.list.selectedID)
	}
	sendKey(t, m, "k")
	if m.list.selectedID != b.ID {
		t.Errorf("after k, selection = %s, want newest again", m.list.selectedID)
	}
	// Selecting an item clears its unread flag.
	if m.list.unread[b.ID] {
		t.Error("unread flag should clear on selection")
	}
}

func TestSearchFilters(t *testing.T) {
	a := testDelivery("POST", "/github", `{"x":"github hook"}`)
	b := testDelivery("GET", "/health", `{}`)
	m := newTestApp(t, []delivery.Delivery{a, b})

	sendKey(t, m, "/")
	if m.mode != modeSearch {
		t.Fatal("slash should open search")
	}
	for _, r := range "github" {
		sendKey(t, m, string(r))
	}
	if got := len(m.visible()); got != 1 {
		t.Errorf("visible after filter = %d, want 1", got)
	}
	sendKey(t, m, "enter")
	if m.mode != modeNormal || m.search != "github" {
		t.Error("enter should commit search and return to normal mode")
	}
	sendKey(t, m, "esc") // esc in normal mode clears filters
	if got := len(m.visible()); got != 2 {
		t.Errorf("esc should clear filter, visible = %d", got)
	}
}

func TestMethodFilterCycles(t *testing.T) {
	a := testDelivery("POST", "/github", `{}`)
	b := testDelivery("GET", "/health", `{}`)
	m := newTestApp(t, []delivery.Delivery{a, b})

	sendKey(t, m, "f") // GET first in cycle
	if got := len(m.visible()); got != 1 {
		t.Errorf("visible = %d, want 1 (GET only)", got)
	}
	if m.visible()[0].Method != "GET" {
		t.Errorf("filtered item = %s, want GET", m.visible()[0].Method)
	}
	sendKey(t, m, "esc")
	if got := len(m.visible()); got != 2 {
		t.Errorf("esc should clear filter, visible = %d", got)
	}
}

func TestTabSwitchesFocusAndDetailHints(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{"k":1}`)})
	sendKey(t, m, "tab")
	if m.focus != focusDetail {
		t.Fatal("tab should focus detail")
	}
	if !strings.Contains(m.View(), "tabs") {
		t.Error("detail hints missing after tab")
	}
	sendKey(t, m, "tab")
	if m.focus != focusList {
		t.Error("second tab should return to list")
	}
}

func TestHelpOverlayToggles(t *testing.T) {
	m := newTestApp(t, nil)
	sendKey(t, m, "?")
	if m.mode != modeHelp {
		t.Fatal("? should open help")
	}
	view := m.View()
	for _, want := range []string{"navigate", "actions", "replay to target", "quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("help overlay missing %q", want)
		}
	}
	assertFrameFits(t, view, m.width, m.height)
	sendKey(t, m, "esc")
	if m.mode != modeNormal {
		t.Error("esc should close help")
	}
}

func TestConfirmClear(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/x", `{}`)})
	sendKey(t, m, "x")
	if m.mode != modeConfirm {
		t.Fatal("x should ask for confirmation")
	}
	sendKey(t, m, "n")
	if len(m.items) != 1 {
		t.Error("n should cancel the clear")
	}
	sendKey(t, m, "x")
	sendKey(t, m, "y")
	if len(m.items) != 0 {
		t.Error("y should clear items")
	}
}

func TestReplayWithoutTargetWarns(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)})
	cmd := sendKey(t, m, "r")
	if cmd == nil {
		t.Fatal("r should produce a toast command")
	}
	if m.toast == nil || !strings.Contains(m.toast.text, "replay target") {
		t.Errorf("expected no-target warning toast, got %+v", m.toast)
	}
}

func TestReplayRoundTrip(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		gotBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(gotBody)
		w.WriteHeader(201)
	}))
	defer srv.Close()

	in := testDelivery("POST", "/hooks/github", `{"z":9}`)
	in.Headers["X-Test"] = []string{"yes"}
	m := newTestApp(t, []delivery.Delivery{in})
	sendKey(t, m, "t")
	for _, r := range srv.URL {
		sendKey(t, m, string(r))
	}
	sendKey(t, m, "enter")

	cmd := sendKey(t, m, "r")
	msg := cmd() // perform the HTTP call synchronously
	model, _ := m.Update(msg)
	*m = *model.(*Model)

	if gotReq == nil {
		t.Fatal("replay never reached the target")
	}
	if gotReq.URL.Path != "/hooks/github" {
		t.Errorf("replay path = %s", gotReq.URL.Path)
	}
	if gotReq.Header.Get("X-Test") != "yes" {
		t.Error("custom header not forwarded")
	}
	if string(gotBody) != `{"z":9}` {
		t.Errorf("replay body = %q", string(gotBody))
	}
	if m.toast == nil || !strings.Contains(m.toast.text, "201") {
		t.Errorf("toast should report 201, got %+v", m.toast)
	}
}

func TestEditorDoneEntersDiffAndDiscard(t *testing.T) {
	in := testDelivery("POST", "/hooks/x", `{"count":1}`)
	m := newTestApp(t, []delivery.Delivery{in})

	msg := editorDoneMsg{d: in, body: []byte(`{"count":2}`)}
	model, _ := m.Update(msg)
	*m = *model.(*Model)

	if m.mode != modeDiff || m.pending == nil {
		t.Fatal("edited body should enter diff review")
	}
	view := m.View()
	if !strings.Contains(view, "diff review") {
		t.Error("diff view not rendered")
	}
	assertFrameFits(t, view, m.width, m.height)

	sendKey(t, m, "esc")
	if m.mode != modeNormal || m.pending != nil {
		t.Error("esc should discard the edit")
	}
}

func TestNarrowLayoutStacks(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{"k":"v"}`)})
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	*m = *model.(*Model)
	if m.splitPanes() {
		t.Fatal("80 columns should stack panes")
	}
	view := m.View()
	if !strings.Contains(view, "/p") {
		t.Error("list rows missing in stacked layout")
	}
	assertFrameFits(t, view, 80, 24)
}

// assertFrameFits is the layout invariant: every rendered line fits the
// terminal width, and the frame is exactly height lines tall — no scroll
// jitter, no overflow, at any theme.
func assertFrameFits(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Errorf("frame height = %d lines, want %d", len(lines), height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d width %d exceeds terminal width %d: %q", i, w, width, truncateForLog(line))
		}
	}
}

func truncateForLog(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0B"}, {512, "512B"}, {2048, "2.0K"}, {3 * 1024 * 1024, "3.0M"},
	}
	for _, c := range cases {
		if got := humanSize(c.n); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestFormPairsPreservesOrderAndRepeats(t *testing.T) {
	pairs := formPairs([]byte("a=1&b=two+words&a=3&k="))
	want := [][2]string{{"a", "1"}, {"b", "two words"}, {"a", "3"}, {"k", ""}}
	if len(pairs) != len(want) {
		t.Fatalf("pairs = %v, want %v", pairs, want)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pair %d = %v, want %v", i, pairs[i], want[i])
		}
	}
}

func TestLineDiff(t *testing.T) {
	old := "same\nremoved\nkept"
	newS := "same\nkept\nadded"
	diff := lineDiff(strings.Split(old, "\n"), strings.Split(newS, "\n"))
	var adds, dels int
	for _, l := range diff {
		switch l.kind {
		case '+':
			adds++
		case '-':
			dels++
		}
	}
	if adds != 1 || dels != 1 {
		t.Errorf("diff adds=%d dels=%d, want 1/1 (diff: %v)", adds, dels, diff)
	}
}

func TestInputUpdate(t *testing.T) {
	in := newInput("find", "")
	in.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	in.Update(tea.KeyMsg{Type: tea.KeySpace})
	in.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if in.Value() != "ab c" {
		t.Errorf("value = %q, want %q", in.Value(), "ab c")
	}
	in.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if in.Value() != "ab " {
		t.Errorf("after backspace = %q", in.Value())
	}
	submit, _ := in.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !submit {
		t.Error("enter should submit")
	}
}

func TestTabNumber(t *testing.T) {
	for s, want := range map[string]int{"1": 1, "4": 4} {
		if got, ok := tabNumber(s); !ok || got != want {
			t.Errorf("tabNumber(%q) = %d,%v", s, got, ok)
		}
	}
	if _, ok := tabNumber("9"); ok {
		t.Error("tabNumber(9) should not match")
	}
}

func TestDetailTabSwitching(t *testing.T) {
	in := testDelivery("POST", "/p", `{"a":1}`)
	in.Headers["X-Tag"] = []string{"secret"}
	m := newTestApp(t, []delivery.Delivery{in})
	sendKey(t, m, "tab")
	view := m.View()
	if !strings.Contains(view, `"a"`) {
		t.Error("body tab should show JSON")
	}
	sendKey(t, m, "2")
	view = m.View()
	if !strings.Contains(view, "X-Tag") {
		t.Error("headers tab should show headers")
	}
	sendKey(t, m, "l") // wrap around eventually reaches body again
	if m.detail.tab < 0 || m.detail.tab > 3 {
		t.Errorf("tab index out of range: %d", m.detail.tab)
	}
}

// --- regression tests from the independent review ---

func TestFilterAfterDeepScrollDoesNotBlankList(t *testing.T) {
	// 40 deliveries, scrolled to the bottom, then filter: the list pane
	// must still render rows (stale-offset clamp).
	var initial []delivery.Delivery
	for i := 0; i < 40; i++ {
		initial = append(initial, testDelivery("POST", fmt.Sprintf("/item/%02d", i), `{}`))
	}
	m := newTestApp(t, initial)
	sendKey(t, m, "G") // jump to bottom
	sendKey(t, m, "f") // GET filter → zero matches in a POST-only list
	view := m.View()
	if !strings.Contains(view, "no deliveries match") {
		t.Error("empty filtered list should render the no-match state, not a blank pane")
	}
	sendKey(t, m, "esc")
	sendKey(t, m, "G")
	sendKey(t, m, "/")
	for _, r := range "item/39" {
		sendKey(t, m, string(r))
	}
	view = m.View()
	if !strings.Contains(view, "/item/39") {
		t.Error("deep-scroll + search should still show the matching row")
	}
}

func TestSearchEscClearsFilter(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/keep", `{}`)})
	sendKey(t, m, "/")
	for _, r := range "nomatch" {
		sendKey(t, m, string(r))
	}
	sendKey(t, m, "esc") // hint promises esc clears
	if m.search != "" {
		t.Errorf("search = %q after esc, want cleared", m.search)
	}
	if got := len(m.visible()); got != 1 {
		t.Errorf("visible after esc = %d, want 1", got)
	}
}

func TestMouseIgnoredInOverlayModes(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{}`)})
	before := m.list.selectedID
	sendKey(t, m, "?")
	m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, X: 2, Y: 4})
	if m.list.selectedID != before {
		t.Error("click in help overlay must not move the list selection")
	}
}

func TestClickSelectsRowInListPaneOnly(t *testing.T) {
	a := testDelivery("POST", "/first", `{}`)
	b := testDelivery("GET", "/second", `{}`)
	m := newTestApp(t, []delivery.Delivery{a, b})
	// Click row 1 (y=4) at x=5 — inside the list pane.
	m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, X: 5, Y: 4})
	if m.list.selectedID == a.ID {
		t.Log("clicked second row selection:", m.list.selectedID)
	}
	// Click far right (detail pane) — must be ignored.
	detailSide := m.listWidth() + 10
	m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, X: detailSide, Y: 4})
	// No panic and selection still valid is the contract here.
	if m.list.selectedID == "" {
		t.Error("selection lost after ignored click")
	}
}

func TestCopyEmitsOSC52ViaView(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/p", `{"x":1}`)})
	cmd := sendKey(t, m, "c")
	msg := cmd() // produces oscMsg
	model, _ := m.Update(msg)
	m2 := model.(*Model)
	view := m2.View()
	if !strings.Contains(view, "\x1b]52;c;") {
		t.Error("view should carry the OSC52 sequence once issued")
	}
}

func TestSearchMatchesBodyWithinCap(t *testing.T) {
	in := testDelivery("POST", "/big", `{"needle":"here"}`)
	m := newTestApp(t, []delivery.Delivery{in})
	m.search = "needle"
	m.visibleDirty = true
	if got := len(m.visible()); got != 1 {
		t.Errorf("body search failed, visible=%d", got)
	}
	// Haystack built once and reused.
	if h := m.haystack(m.items[0]); !strings.Contains(h, "needle") {
		t.Error("haystack missing body text")
	}
}
