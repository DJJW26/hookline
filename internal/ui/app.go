// Package ui implements the Bubble Tea interface: one app model owning a
// list pane, a detail pane, an inline input, a help overlay and a status
// line. All colours and shapes come from internal/style; all capture,
// persistence and replay logic lives in the core packages and reaches this
// layer only as messages.
package ui

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DJJW26/hookline/internal/delivery"
	"github.com/DJJW26/hookline/internal/replay"
	"github.com/DJJW26/hookline/internal/sign"
	"github.com/DJJW26/hookline/internal/style"
)

// CaptureEvent carries a live delivery and the outcome of saving it to history.
// HistoryGeneration advances after each successful clear.
type CaptureEvent struct {
	Delivery          delivery.Delivery
	HistoryError      error
	HistoryGeneration uint64
}

// Config wires the app to its environment. Everything here is injectable,
// which is what keeps the UI testable without a terminal or a network.
type Config struct {
	Version    string
	ListenURL  string // "" when not listening (offline viewing mode)
	Events     <-chan CaptureEvent
	Initial    []delivery.Delivery
	Target     string                 // default replay target ("" = unset)
	OnClear    func() (uint64, error) // clears history and returns the new generation
	TargetSave func(target string)    // persists a new replay target (wired to config.json)
	replayDo   func(context.Context, string, delivery.Delivery, ...replay.Option) (replay.Result, error)
	// SignProvider enables signature re-signing on replay (from --sign):
	// "" disables it; otherwise one of the internal/sign providers. When
	// set, SignSecret (from HOOKLINE_SIGN_SECRET) must be too — a replay
	// asked for signing but missing the secret is never sent.
	SignProvider string
	SignSecret   string
	// Dropped, when non-nil, points at the live count of deliveries the
	// capture callback dropped because the events channel was full. The
	// status line shows the count whenever it is non-zero — drops are
	// surfaced, never silent.
	Dropped *atomic.Int64
	// HistoryWriteFailures counts all failed saves, even if the UI queue was full.
	HistoryWriteFailures *atomic.Int64
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg   Config
	st    style.Styles
	theme style.Theme

	items             []*delivery.Delivery // newest first
	list              listModel
	detail            detailModel
	help              helpModel
	unsaved           map[string]bool
	historyGeneration uint64

	// sessionBytes is the running total of body bytes held in items,
	// maintained incrementally by the delivery path and evictOld so the
	// maxSessionBytes cap is enforced without re-summing every body on
	// each capture.
	sessionBytes int

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

	// replayByID remembers the most recent replay outcome per delivery
	// (row badges + the response tab); bounded by replayMaxEntries.
	replayByID map[string]replayInfo

	target        string
	pending       *pendingEdit
	pendingReplay *pendingReplay
	toast         *toast
	pendingOSC    string

	width, height int
}

// replayInfo is one replay outcome, kept for badges and the response tab.
type replayInfo struct {
	target string
	res    replay.Result
}

// replayMaxEntries bounds replay memory; older outcomes are dropped.
const replayMaxEntries = 64

// maxSessionBytes bounds the bodies held in memory during a session so a
// burst of multi-megabyte captures cannot grow the heap without limit.
// Older deliveries are evicted (they remain on disk in the store).
const maxSessionBytes = 256 << 20

// searchBodyCap limits how much of a body participates in substring
// search; beyond this, matches in the visible head are what matter.
const searchBodyCap = 64 << 10

// New constructs the app model.
func New(cfg Config, st style.Styles, theme style.Theme) *Model {
	if cfg.replayDo == nil {
		cfg.replayDo = replay.Do
	}
	items := make([]*delivery.Delivery, 0, len(cfg.Initial)+16)
	// Newest first: reverse the chronological initial load.
	var seeded int
	for i := len(cfg.Initial) - 1; i >= 0; i-- {
		d := cfg.Initial[i]
		seeded += len(d.Body)
		items = append(items, &d)
	}
	m := &Model{
		cfg:          cfg,
		st:           st,
		theme:        theme,
		items:        items,
		sessionBytes: seeded,
		list:         newListModel(),
		detail:       newDetailModel(),
		methodCycle:  []string{"", "GET", "POST", "PUT", "PATCH", "DELETE"},
		target:       cfg.Target,
		searchCache:  map[string]string{},
		replayByID:   map[string]replayInfo{},
		visibleDirty: true,
		unsaved:      map[string]bool{},
	}
	if len(items) > 0 {
		m.list.selectedID = items[0].ID // reset selection to the newest delivery
	}
	m.evictOld()
	return m
}

// sessionBytes sums the bodies currently held in items. evictOld tracks
// this total incrementally on Model.sessionBytes; the explicit sum remains
// as the reference the tests cross-check the running counter against.
func sessionBytes(items []*delivery.Delivery) int {
	var n int
	for _, d := range items {
		n += len(d.Body)
	}
	return n
}

// evictOld drops the oldest deliveries until the session stays under
// maxSessionBytes, keeping the running sessionBytes counter and every
// per-delivery cache (search haystacks, unread marks, pretty JSON, replay
// outcomes) in lockstep — an entry whose delivery is gone is dead weight.
func (m *Model) evictOld() {
	for m.sessionBytes > maxSessionBytes && len(m.items) > 1 {
		old := m.items[len(m.items)-1]
		m.sessionBytes -= len(old.Body)
		m.items = m.items[:len(m.items)-1]
		delete(m.searchCache, old.ID)
		m.list.forget(old.ID)
		delete(m.detail.prettyCache, old.ID)
		delete(m.replayByID, old.ID)
		delete(m.unsaved, old.ID)
	}
	m.visibleDirty = true
}

// waitForDelivery turns channel reads into messages; re-armed after every
// delivery so the pump never stalls.
func waitForDelivery(ch <-chan CaptureEvent) tea.Cmd {
	return func() tea.Msg {
		d, ok := <-ch
		if !ok {
			return deliveriesDoneMsg{}
		}
		return d
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

	case CaptureEvent:
		wait := waitForDelivery(m.cfg.Events)
		if msg.HistoryGeneration < m.historyGeneration {
			return m, wait
		}
		d := msg.Delivery
		m.items = append([]*delivery.Delivery{&d}, m.items...)
		m.sessionBytes += len(d.Body)
		m.list.markUnread(d.ID)
		if msg.HistoryError != nil {
			m.unsaved[d.ID] = true
		}
		m.evictOld()
		if msg.HistoryError != nil {
			return m, tea.Batch(wait, m.showToast(toastBad, "captured live · save failed", d.Method+" "+d.URL()+" · "+msg.HistoryError.Error()))
		}
		return m, wait

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
	case modeReplayConfirm:
		return m.updateReplayConfirm(msg)
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
		case "pgdown", "ctrl+d":
			m.detail.scroll += max(1, m.bodyHeight()-4)
			return m, nil
		case "pgup", "ctrl+u":
			m.detail.scroll -= max(1, m.bodyHeight()-4)
			if m.detail.scroll < 0 {
				m.detail.scroll = 0
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
	case "pgdown", "ctrl+d":
		m.list.move(m.visible(), max(1, m.listContentH()), m.listContentH())
	case "pgup", "ctrl+u":
		m.list.move(m.visible(), -max(1, m.listContentH()), m.listContentH())
	case "g":
		m.list.jumpTo(m.visible(), false, m.listContentH())
	case "G":
		m.list.jumpTo(m.visible(), true, m.listContentH())
	case "n":
		m.jumpUnread()
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
		m.input = newInput(m.input.label, "") // never leak prompt text
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
		m.target = normalizeTarget(strings.TrimSpace(m.input.Value()))
		m.pendingReplay = nil
		if m.cfg.TargetSave != nil && m.target != "" {
			m.cfg.TargetSave(m.target)
		}
		m.mode = modeNormal
		if m.target == "" {
			return m, m.showToast(toastNeutral, "", "replay target cleared")
		}
		return m, m.showToast(toastNeutral, "", "replay target "+m.target)
	}
	m.mode = modeNormal
	return m, nil
}

// normalizeTarget adds the http:// scheme when the user omitted it, so
// "localhost:3000" works instead of failing at replay time.
func normalizeTarget(t string) string {
	if t != "" && !strings.Contains(t, "://") {
		return "http://" + t
	}
	return t
}

func (m *Model) updateDiff(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "s", "enter":
		p := m.pending
		m.pending = nil
		m.mode = modeNormal
		if p != nil {
			return m, m.prepareReplay(p.next, p)
		}
	case "esc":
		m.pending = nil
		m.mode = modeNormal
	}
	return m, nil
}

func (m *Model) updateReplayConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.pendingReplay
	if p == nil {
		m.mode = modeNormal
		return m, nil
	}
	if p.target != m.target {
		m.cancelReplay(p)
		return m, nil
	}
	switch msg.String() {
	case "Y":
		if _, fits := m.replayConfirmLines(p); fits {
			m.pendingReplay = nil
			m.mode = modeNormal
			return m, m.sendReplay(p.delivery, p.target)
		}
	case "n", "N", "esc":
		m.cancelReplay(p)
	case "q", "ctrl+c":
		m.cancelReplay(p)
		return m, tea.Quit
	case "t":
		m.cancelReplay(p)
		if p.edit == nil {
			return m.updateNormal(msg)
		}
	}
	return m, nil
}

func (m *Model) cancelReplay(p *pendingReplay) {
	m.pendingReplay = nil
	m.mode = modeNormal
	if p.edit != nil {
		m.pending = p.edit
		m.mode = modeDiff
	}
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = modeNormal
		if m.cfg.OnClear != nil {
			generation, err := m.cfg.OnClear()
			if err != nil {
				return m, m.showToast(toastBad, "clear failed", err.Error())
			}
			m.historyGeneration = generation
		}
		m.items = nil
		m.sessionBytes = 0 // the bodies it tracks are gone
		m.list = newListModel()
		m.searchCache = map[string]string{}
		m.detail.prettyCache = map[string]string{}
		m.replayByID = map[string]replayInfo{}
		m.visibleDirty = true
		clear(m.unsaved)
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
	// Remember the outcome: it becomes the row badge and the response tab.
	if len(m.replayByID) >= replayMaxEntries {
		m.replayByID = map[string]replayInfo{}
	}
	m.replayByID[msg.d.ID] = replayInfo{target: msg.target, res: msg.res}
	m.detail.cacheKey = "" // response tab content changed

	kind := toastGood
	switch {
	case msg.res.StatusCode >= 400:
		kind = toastBad
	case msg.res.StatusCode >= 300:
		kind = toastWarn
	}
	text := fmt.Sprintf("%d in %s — response tab for body", msg.res.StatusCode, msg.res.Duration.Round(time.Millisecond))
	if msg.resigned != "" {
		// The send completed (any status class): the request went out
		// freshly signed, which is what the toast should say.
		text += fmt.Sprintf(" · re-signed (%s)", msg.resigned)
	}
	return m, m.showToast(kind, subject, text)
}

func (m *Model) applyEditorDone(msg editorDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, m.showToast(toastBad, "", "editor: "+msg.err.Error())
	}
	if string(msg.body) == string(msg.d.Body) {
		return m, m.showToast(toastNeutral, "", "no changes")
	}
	next := msg.d // keep the original ID so its row shows the replay response
	next.Body = msg.body
	m.pendingReplay = nil
	m.pending = &pendingEdit{orig: msg.d, next: next}
	m.mode = modeDiff
	return m, nil
}

func (m *Model) replaySelected(d delivery.Delivery) tea.Cmd {
	return m.prepareReplay(d, nil)
}

// prepareReplay checks the request and prompts before sending to a remote host.
func (m *Model) prepareReplay(d delivery.Delivery, edit *pendingEdit) tea.Cmd {
	if d.Truncated {
		return m.showToast(toastWarn, "", replay.ErrTruncatedBody.Error())
	}
	if m.target == "" {
		return m.showToast(toastWarn, "", "no replay target — press t to set one")
	}
	if m.cfg.SignProvider != "" && m.cfg.SignSecret == "" {
		return m.showToast(toastWarn, "", "--sign "+m.cfg.SignProvider+
			" is set but HOOKLINE_SIGN_SECRET is empty — replay not sent")
	}
	if !isLocalTarget(replay.RequestURL(m.target, d)) {
		m.pendingReplay = &pendingReplay{delivery: d, target: m.target, edit: edit}
		m.mode = modeReplayConfirm
		m.toast = nil
		return nil
	}
	return m.sendReplay(d, m.target)
}

func (m *Model) sendReplay(d delivery.Delivery, target string) tea.Cmd {
	provider, secret := m.cfg.SignProvider, m.cfg.SignSecret
	do := m.cfg.replayDo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var opts []replay.Option
		if provider != "" {
			opts = append(opts, replay.WithSigning(sign.Params{
				Provider: provider,
				Secret:   secret,
				Time:     time.Now(),
			}))
		}
		res, err := do(ctx, target, d, opts...)
		return replayResultMsg{d: d, target: target, res: res, err: err, resigned: provider}
	}
}

// isLocalTarget reports whether the target points at this machine.
func isLocalTarget(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (m *Model) copyCurl(d delivery.Delivery) tea.Cmd {
	if d.Truncated {
		return m.showToast(toastWarn, "", "body was truncated — capture the complete request before exporting cURL")
	}
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

// haystack builds (once per delivery) the lowercase text searched by "/":
// method, URL, headers and the first searchBodyCap bytes of body — finding
// "the delivery with the weird X-Shopify-Hmac header" is a real workflow.
func (m *Model) haystack(d *delivery.Delivery) string {
	if h, ok := m.searchCache[d.ID]; ok {
		return h
	}
	var hb strings.Builder
	for k, vs := range d.Headers {
		hb.WriteString(k)
		hb.WriteString(": ")
		hb.WriteString(strings.Join(vs, ","))
		hb.WriteString(" ")
	}
	body := d.Body
	if len(body) > searchBodyCap {
		body = body[:searchBodyCap]
	}
	h := strings.ToLower(d.Method + " " + d.URL() + " " + hb.String() + " " + string(body))
	m.searchCache[d.ID] = h
	return h
}

// jumpUnread selects the next delivery marked unread, wrapping past the
// end — the "what did I miss" key after a burst.
func (m *Model) jumpUnread() {
	vis := m.visible()
	if len(vis) == 0 {
		return
	}
	cur := m.list.selectedIndex(vis)
	for step := 1; step <= len(vis); step++ {
		idx := (cur + step) % len(vis)
		if m.list.unread[vis[idx].ID] {
			m.list.selectedID = vis[idx].ID
			delete(m.list.unread, vis[idx].ID)
			m.list.scrollTo(idx, len(vis), m.listContentH())
			return
		}
	}
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
