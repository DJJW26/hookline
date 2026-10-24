// Package ui implements the Bubble Tea interface: one app model owning a
// list pane, a detail pane, an inline input, a help overlay and a status
// line. All colours and shapes come from internal/style; all capture,
// persistence and replay logic lives in the core packages and reaches this
// layer only as messages.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hookline/internal/delivery"
	"hookline/internal/replay"
	"hookline/internal/style"
)

// Config wires the app to its environment. Everything here is injectable,
// which is what keeps the UI testable without a terminal or a network.
type Config struct {
	Version   string
	ListenURL string // "" when not listening (offline viewing mode)
	Events    <-chan delivery.Delivery
	Initial   []delivery.Delivery
	Target    string // default replay target ("" = unset)
	OnClear   func() // persists a clear request (wired to the store)
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg   Config
	st    style.Styles
	theme style.Theme

	items  []*delivery.Delivery // newest first
	list   listModel
	detail detailModel
	help   helpModel

	focus focus
	mode  mode

	input       inputModel
	methodCycle []string // filter cycle order for 'f'
	methodIdx   int      // 0 = no filter
	search      string

	// visibleItems caches the filtered view; recompute only when items or
	// filters change (search runs over cached haystacks, never per frame).
	visibleItems []*delivery.Delivery
	visibleDirty bool
	searchCache  map[string]string // delivery ID → lowercased haystack

	target     string
	pending    *pendingEdit
	toast      *toast
	pendingOSC string

	width, height int
}

// maxSessionBytes bounds the bodies held in memory during a session so a
// burst of multi-megabyte captures cannot grow the heap without limit.
// Older deliveries are evicted (they remain on disk in the store).
const maxSessionBytes = 256 << 20

// searchBodyCap limits how much of a body participates in substring
// search; beyond this, matches in the visible head are what matter.
const searchBodyCap = 64 << 10

// New constructs the app model.
func New(cfg Config, st style.Styles, theme style.Theme) *Model {
	items := make([]*delivery.Delivery, 0, len(cfg.Initial)+16)
	// Newest first: reverse the chronological initial load.
	for i := len(cfg.Initial) - 1; i >= 0; i-- {
		d := cfg.Initial[i]
		items = append(items, &d)
	}
	m := &Model{
		cfg:          cfg,
		st:           st,
		theme:        theme,
		items:        items,
		list:         newListModel(),
		detail:       newDetailModel(),
		methodCycle:  []string{"", "GET", "POST", "PUT", "PATCH", "DELETE"},
		target:       cfg.Target,
		searchCache:  map[string]string{},
		visibleDirty: true,
	}
	if len(items) > 0 {
		m.list.selectedID = items[0].ID // reset selection to the newest delivery
	}
	m.evictOld()
	return m
}

// sessionBytes sums the bodies currently held in memory.
func sessionBytes(items []*delivery.Delivery) int {
	var n int
	for _, d := range items {
		n += len(d.Body)
	}
	return n
}

// evictOld drops the oldest deliveries until the session stays under
// maxSessionBytes.
func (m *Model) evictOld() {
	for sessionBytes(m.items) > maxSessionBytes && len(m.items) > 1 {
		m.items = m.items[:len(m.items)-1]
	}
	m.visibleDirty = true
}

// waitForDelivery turns channel reads into messages; re-armed after every
// delivery so the pump never stalls.
func waitForDelivery(ch <-chan delivery.Delivery) tea.Cmd {
	return func() tea.Msg {
		d, ok := <-ch
		if !ok {
			return deliveriesDoneMsg{}
		}
		return deliveryMsg(d)
	}
}

func toastTick() tea.Cmd {
	return tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{} })
}

// Init starts the event pump and sets the terminal window title.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForDelivery(m.cfg.Events)}
	title := "hookline"
	if m.cfg.ListenURL != "" {
		title += " — " + strings.TrimPrefix(m.cfg.ListenURL, "http://")
	}
	cmds = append(cmds, tea.SetWindowTitle(title))
	return tea.Batch(cmds...)
}

// Update reduces one message into the next state. Layout recalculations and
// every side effect are collected here, keeping View a pure renderer.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case deliveryMsg:
		d := delivery.Delivery(msg)
		m.items = append([]*delivery.Delivery{&d}, m.items...)
		m.list.markUnread(d.ID)
		m.evictOld()
		return m, waitForDelivery(m.cfg.Events)

	case deliveriesDoneMsg:
		return m, nil

	case replayResultMsg:
		return m.applyReplayResult(msg)

	case editorDoneMsg:
		return m.applyEditorDone(msg)

	case toastExpiredMsg:
		m.toast = nil
		return m, nil

	case clipDoneMsg:
		if msg.err != nil {
			return m, m.showToast(toastBad, "", msg.err.Error())
		}
		return m, m.showToast(toastGood, "", "copied "+msg.what)

	case oscMsg:
		// The sequence rides along in View output; the flush must wait
		// until the renderer has painted at least one frame (~60fps
		// ticker), so clear on a tick comfortably past that, not on the
		// next message.
		m.pendingOSC = string(msg)
		return m, tea.Tick(oscFlushDelay, func(time.Time) tea.Msg { return oscFlushMsg{} })

	case oscFlushMsg:
		m.pendingOSC = ""
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Input modes swallow keys first.
	switch m.mode {
	case modeSearch, modeTarget:
		return m.updateInput(msg)
	case modeDiff:
		return m.updateDiff(msg)
	case modeConfirm:
		return m.updateConfirm(msg)
	case modeHelp:
		return m.updateHelp(msg)
	}
	return m.updateNormal(msg)
}

func (m *Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Pane-scoped navigation first.
	if m.focus == focusDetail {
		switch msg.String() {
		case "tab", "shift+tab":
			m.focus = focusList
			return m, nil
		case "h", "left", "1", "2", "3", "4":
			if n, ok := tabNumber(msg.String()); ok {
				m.detail.setTab(n - 1)
			} else {
				m.detail.nextTab(-1)
			}
			return m, nil
		case "l", "right":
			m.detail.nextTab(1)
			return m, nil
		case "j", "down":
			m.detail.scroll++
			return m, nil
		case "k", "up":
			if m.detail.scroll > 0 {
				m.detail.scroll--
			}
			return m, nil
		case "esc":
			m.focus = focusList
			return m, nil
		}
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		return m, nil
	case "tab":
		m.focus = focusDetail
		return m, nil
	case "shift+tab":
		m.focus = focusList
		return m, nil
	}

	// List-focused actions.
	switch msg.String() {
	case "j", "down":
		m.list.move(m.visible(), 1, m.listContentH())
	case "k", "up":
		m.list.move(m.visible(), -1, m.listContentH())
	case "g":
		m.list.jumpTo(m.visible(), false, m.listContentH())
	case "G":
		m.list.jumpTo(m.visible(), true, m.listContentH())
	case "enter":
		if len(m.visible()) > 0 {
			m.focus = focusDetail
		}
	case "r":
		if sel := m.selection(); sel != nil {
			return m, m.replaySelected(*sel)
		}
	case "R":
		if len(m.items) > 0 {
			return m, m.replaySelected(*m.items[0])
		}
	case "e":
		if sel := m.selection(); sel != nil {
			return m, openEditor(*sel)
		}
	case "c":
		if sel := m.selection(); sel != nil {
			return m, m.copyCurl(*sel)
		}
	case "y":
		if sel := m.selection(); sel != nil {
			return m, copyText(string(sel.Body), "body")
		}
	case "t":
		m.mode = modeTarget
		m.input = newInput("target", m.target)
		return m, nil
	case "/":
		m.mode = modeSearch
		m.input = newInput("find", "")
		return m, nil
	case "f":
		m.methodIdx = (m.methodIdx + 1) % len(m.methodCycle)
		m.visibleDirty = true
	case "x":
		if len(m.items) > 0 {
			m.mode = modeConfirm
		}
	case "esc":
		m.methodIdx = 0
		m.search = ""
		m.visibleDirty = true
	}
	return m, nil
}

func (m *Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	submit, cancel := m.input.Update(msg)
	if cancel {
		// Esc in the find prompt also clears the filter, as the on-screen
		// hint promises.
		if m.mode == modeSearch {
			m.search = ""
			m.visibleDirty = true
		}
		m.mode = modeNormal
		return m, nil
	}
	if !submit {
		if m.mode == modeSearch {
			m.search = m.input.Value() // live filtering while typing
			m.visibleDirty = true
		}
		return m, nil
	}
	// Enter submitted the prompt.
	if m.mode == modeTarget {
		m.target = strings.TrimSpace(m.input.Value())
		m.mode = modeNormal
		return m, m.showToast(toastNeutral, "", "replay target "+m.target)
	}
	m.mode = modeNormal
	return m, nil
}

func (m *Model) updateDiff(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "s", "enter":
		p := m.pending
		m.pending = nil
		m.mode = modeNormal
		if p != nil {
			return m, m.replaySelected(p.next)
		}
	case "esc":
		m.pending = nil
		m.mode = modeNormal
	}
	return m, nil
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = modeNormal
		m.items = nil
		m.list = newListModel()
		m.searchCache = map[string]string{}
		m.visibleDirty = true
		if m.cfg.OnClear != nil {
			m.cfg.OnClear()
		}
		return m, m.showToast(toastNeutral, "", "cleared")
	case "n", "N", "esc":
		m.mode = modeNormal
	}
	return m, nil
}

func (m *Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "q", "esc":
		m.mode = modeNormal
	}
	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Overlays capture the screen; clicks and wheels there must not move
	// the hidden list behind them.
	if m.mode != modeNormal {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		up := msg.Button == tea.MouseButtonWheelUp
		if m.focus == focusDetail {
			if up && m.detail.scroll > 0 {
				m.detail.scroll--
			} else if !up {
				m.detail.scroll++
			}
		} else {
			m.list.wheel(m.visible(), up, m.listContentH())
		}
	case tea.MouseButtonLeft:
		if idx, ok := m.listRowAt(msg.X, msg.Y); ok {
			vis := m.visible()
			if idx >= 0 && idx < len(vis) {
				m.list.selectedID = vis[idx].ID
				m.focus = focusList
			}
		}
	}
	return m, nil
}

// listRowAt maps a click at (x, y) to a visible row index, accounting for
// the header line, the panel border, the title row, and the pane the
// click actually landed in. Row content spans screen lines [3, 3+contentH).
func (m *Model) listRowAt(x, y int) (int, bool) {
	const topOffset = 3 // header + panel top border + title row
	if y < topOffset || y >= topOffset+m.listContentH() {
		return 0, false
	}
	if m.splitPanes() && x > m.listWidth() {
		// In split layout only the left pane belongs to the list.
		return 0, false
	}
	return m.list.offset + (y - topOffset), true
}

func (m *Model) applyReplayResult(msg replayResultMsg) (tea.Model, tea.Cmd) {
	subject := msg.d.Method + " " + msg.d.URL()
	if msg.err != nil {
		return m, m.showToast(toastBad, subject, msg.err.Error())
	}
	text := fmt.Sprintf("%d in %s", msg.res.StatusCode, msg.res.Duration.Round(time.Millisecond))
	return m, m.showToast(toastGood, subject, text)
}

func (m *Model) applyEditorDone(msg editorDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, m.showToast(toastBad, "", "editor: "+msg.err.Error())
	}
	if string(msg.body) == string(msg.d.Body) {
		return m, m.showToast(toastNeutral, "", "no changes")
	}
	next := msg.d
	next.Body = msg.body
	next.Truncated = false
	next.ID = delivery.NewID() // edited copy, distinct identity in cache keys
	m.pending = &pendingEdit{orig: msg.d, next: next}
	m.mode = modeDiff
	return m, nil
}

// replaySelected sends a delivery to the configured target.
func (m *Model) replaySelected(d delivery.Delivery) tea.Cmd {
	if m.target == "" {
		return m.showToast(toastWarn, "", "no replay target — press t to set one")
	}
	target := m.target
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := replay.Do(ctx, target, d)
		return replayResultMsg{d: d, res: res, err: err}
	}
}

func (m *Model) copyCurl(d delivery.Delivery) tea.Cmd {
	base := m.cfg.ListenURL
	if m.target != "" {
		base = m.target
	}
	cmdText := replay.ToCurl(base, d)
	return copyText(cmdText, "cURL")
}

func (m *Model) showToast(kind toastKind, subject, text string) tea.Cmd {
	m.toast = &toast{kind: kind, subject: subject, text: text}
	return toastTick()
}

// visible returns the filtered deliveries, newest first. The result is
// cached and recomputed only when items or filters change — search runs
// over per-delivery haystacks built once (and body-capped), never by
// lowercasing whole bodies every frame.
func (m *Model) visible() []*delivery.Delivery {
	if !m.visibleDirty {
		return m.visibleItems
	}
	method := ""
	if m.methodIdx > 0 && m.methodIdx < len(m.methodCycle) {
		method = m.methodCycle[m.methodIdx]
	}
	needle := strings.ToLower(m.search)
	if method == "" && needle == "" {
		m.visibleItems = m.items
		m.visibleDirty = false
		return m.visibleItems
	}
	out := make([]*delivery.Delivery, 0, len(m.items))
	for _, d := range m.items {
		if method != "" && !strings.EqualFold(d.Method, method) {
			continue
		}
		if needle != "" && !strings.Contains(m.haystack(d), needle) {
			continue
		}
		out = append(out, d)
	}
	m.visibleItems = out
	m.visibleDirty = false
	return m.visibleItems
}

// haystack builds (once per delivery) the lowercase text searched by "/".
func (m *Model) haystack(d *delivery.Delivery) string {
	if h, ok := m.searchCache[d.ID]; ok {
		return h
	}
	body := d.Body
	if len(body) > searchBodyCap {
		body = body[:searchBodyCap]
	}
	h := strings.ToLower(d.Method + " " + d.URL() + " " + string(body))
	m.searchCache[d.ID] = h
	return h
}

// filterLabel describes the active filter for the list note.
func (m *Model) filterLabel() string {
	var parts []string
	if m.methodIdx > 0 && m.methodIdx < len(m.methodCycle) {
		parts = append(parts, m.methodCycle[m.methodIdx])
	}
	if m.search != "" {
		parts = append(parts, "/"+m.search)
	}
	return strings.Join(parts, " · ")
}

func (m *Model) selection() *delivery.Delivery {
	return selectedDelivery(m.visible(), &m.list)
}

func tabNumber(s string) (int, bool) {
	if len(s) == 1 && s[0] >= '1' && s[0] <= '4' {
		return int(s[0] - '0'), true
	}
	return 0, false
}
