package ui

import (
	"time"

	"github.com/DJJW26/hookline/internal/delivery"
	"github.com/DJJW26/hookline/internal/replay"
)

// Messages exchanged inside the UI. Everything asynchronous (listener
// activity, replay results, editor round-trips) lands here so Update stays
// a pure message reducer.

// deliveryMsg carries one freshly captured delivery from the listener.
type deliveryMsg delivery.Delivery

// deliveriesDoneMsg is sent when the events channel closes.
type deliveriesDoneMsg struct{}

// replayResultMsg reports the outcome of a replay attempt.
type replayResultMsg struct {
	d   delivery.Delivery
	res replay.Result
	err error
}

// editorDoneMsg is sent after $EDITOR exits; body is the edited payload.
type editorDoneMsg struct {
	d    delivery.Delivery
	body []byte
	err  error
}

// toastExpiredMsg clears the status-line toast.
type toastExpiredMsg struct{}

// oscMsg carries a raw terminal escape sequence (OSC52 clipboard writes)
// that View must emit exactly once.
type oscMsg string

// oscFlushMsg clears the pending sequence after it has been rendered.
type oscFlushMsg struct{}

// toast renders on the status line for a few seconds.
type toast struct {
	text    string
	subject string // short context, e.g. "POST /hooks/github"
	kind    toastKind
}

type toastKind int

const (
	toastNeutral toastKind = iota
	toastGood
	toastWarn
	toastBad
)

// toastTTL is how long a toast stays on screen.
const toastTTL = 4 * time.Second

// oscFlushDelay is how long a pending escape sequence stays in View
// output before being cleared — long enough for the renderer (a ~60fps
// ticker) to have painted it at least once.
const oscFlushDelay = 80 * time.Millisecond

// focus names the pane receiving keys.
type focus int

const (
	focusList focus = iota
	focusDetail
)

// mode is the top-level interaction state.
type mode int

const (
	modeNormal  mode = iota
	modeSearch       // typing in the find input
	modeTarget       // typing a replay target
	modeDiff         // reviewing an edited payload before resending
	modeConfirm      // confirming a destructive action
	modeHelp         // help overlay
)

// pendingEdit holds an edited delivery awaiting confirmation in modeDiff.
type pendingEdit struct {
	orig delivery.Delivery
	next delivery.Delivery
}
