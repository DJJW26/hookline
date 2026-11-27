package ui

import (
	"context"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DJJW26/hookline/internal/delivery"
	"github.com/DJJW26/hookline/internal/replay"
)

func TestRemoteReplayConfirmationSendsReviewedEditOnce(t *testing.T) {
	in := testDelivery(http.MethodPost, "/hooks/source", `{"count":1}`)
	in.Query = "source=demo"
	m := newTestApp(t, []delivery.Delivery{in})
	const target = "https://staging.example.test/base/"
	m.target = target
	const edited = `{"count":2}`
	var calls int
	m.cfg.replayDo = func(_ context.Context, gotTarget string, got delivery.Delivery, _ ...replay.Option) (replay.Result, error) {
		calls++
		if gotTarget != target || got.Method != in.Method || got.URL() != in.URL() || string(got.Body) != edited {
			t.Fatalf("replay sent target %q, method %q, path %q and body %q", gotTarget, got.Method, got.URL(), got.Body)
		}
		return replay.Result{StatusCode: http.StatusAccepted}, nil
	}
	m.Update(editorDoneMsg{d: in, body: []byte(edited)})
	if cmd := sendKey(t, m, "s"); cmd != nil || m.mode != modeReplayConfirm {
		t.Fatal("edited replay should prompt before creating a send command")
	}
	if view := m.View(); !strings.Contains(view, "edited") || !strings.Contains(view, in.ID) ||
		!strings.Contains(view, "https://staging.example.test/base/hooks/source?source=demo") {
		t.Fatalf("confirmation lacks the edited request or exact URL:\n%s", view)
	}
	m.Update(CaptureEvent{Delivery: testDelivery(http.MethodPost, "/hooks/newer", "new")})
	if cmd := sendKey(t, m, "R"); cmd != nil {
		t.Fatal("latest delivery key should leave the reviewed request untouched")
	}
	if cmd := sendKey(t, m, "esc"); cmd != nil || m.mode != modeDiff || m.pending == nil || calls != 0 {
		t.Fatal("cancel should make no send and return to the edited draft")
	}
	sendKey(t, m, "s")
	cmd := sendKey(t, m, "Y")
	if cmd == nil || m.pendingReplay != nil || m.mode != modeNormal {
		t.Fatal("confirmation should consume the pending request and schedule a single send")
	}
	if repeated := sendKey(t, m, "Y"); repeated != nil {
		t.Fatal("repeating confirmation should have no effect")
	}
	result := cmd()
	m.target = "https://another.example.test"
	m.Update(result)
	rmsg, ok := result.(replayResultMsg)
	if !ok || calls != 1 || rmsg.d.ID != in.ID || m.replayByID[in.ID].target != target {
		t.Fatal("replay should send once and report the actual target on the original delivery")
	}
	if cmd := sendKey(t, m, "r"); cmd != nil || m.mode != modeReplayConfirm {
		t.Fatal("each new remote replay needs fresh confirmation")
	}
}

func TestRemoteReplayConfirmationInvalidatesAfterTargetChange(t *testing.T) {
	in := testDelivery(http.MethodPost, "/hooks/source", "payload")
	m := newTestApp(t, []delivery.Delivery{in})
	m.target = "https://original.example.test"
	sendKey(t, m, "r")
	if cmd := sendKey(t, m, "t"); cmd != nil || m.mode != modeTarget || m.pendingReplay != nil {
		t.Fatal("opening the target input should invalidate the pending replay")
	}
	m.input = newInput("target", "https://new.example.test")
	sendKey(t, m, "enter")
	m.toast = nil
	if cmd := sendKey(t, m, "r"); cmd != nil || m.mode != modeReplayConfirm || m.pendingReplay.target != m.target {
		t.Fatal("new target needs its own confirmation")
	}
}

func TestRemoteReplayConfirmationRequiresVisibleDestination(t *testing.T) {
	in := testDelivery(http.MethodPost, "/hooks/source", "payload")
	m := newTestApp(t, []delivery.Delivery{in})
	m.target = "https://staging.example.test/" + strings.Repeat("path", 20)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	sendKey(t, m, "r")
	if cmd := sendKey(t, m, "Y"); cmd != nil || m.mode != modeReplayConfirm {
		t.Fatal("confirmation should wait until the destination can be shown in full")
	}
	if body := m.bodyView(); !strings.Contains(body, "Resize to review") {
		t.Fatalf("expected resize hint, got %q", body)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if cmd := sendKey(t, m, "Y"); cmd == nil {
		t.Fatal("confirmation should be available after resizing")
	}
}
