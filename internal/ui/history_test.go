package ui

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DJJW26/hookline/internal/delivery"
)

func TestHistoryWriteFailureKeepsLiveDelivery(t *testing.T) {
	m := newTestApp(t, nil)
	var dropped, failed atomic.Int64
	dropped.Store(2)
	failed.Store(1)
	m.cfg.Dropped = &dropped
	m.cfg.HistoryWriteFailures = &failed

	d := testDelivery("POST", "/failed-save", `{}`)
	m.Update(CaptureEvent{Delivery: d, HistoryError: errors.New("disk full")})
	if len(m.items) != 1 || m.items[0].ID != d.ID || !m.unsaved[d.ID] {
		t.Fatal("failed save should keep the original live capture marked as unsaved")
	}
	if m.toast == nil || !strings.Contains(m.toast.subject, "save failed") || !strings.Contains(m.toast.text, "disk full") {
		t.Fatal("save failure should explain why the capture was not saved")
	}
	m.Update(toastExpiredMsg{})
	view := m.View()
	for _, want := range []string{"!", "2 dropped", "save errors 1"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected view to contain %q after the toast expired", want)
		}
	}
	assertFrameFits(t, view, m.width, m.height)
}

func TestFailedClearKeepsHistoryAndQueuedCaptures(t *testing.T) {
	original := testDelivery("POST", "/original", `{}`)
	m := newTestApp(t, []delivery.Delivery{original})
	m.cfg.OnClear = func() (uint64, error) { return 1, errors.New("access denied") }
	sendKey(t, m, "x")
	sendKey(t, m, "y")
	if len(m.items) != 1 || m.items[0].ID != original.ID || m.sessionBytes != len(original.Body) {
		t.Fatal("failed clear should retain the original history and its state")
	}
	if m.mode != modeNormal || m.toast == nil || m.toast.subject != "clear failed" || m.toast.text != "access denied" {
		t.Fatal("failed clear should return to normal mode with the cause")
	}
	m.Update(CaptureEvent{Delivery: testDelivery("POST", "/queued", `{}`)})
	if len(m.items) != 2 {
		t.Fatal("failed clear should still accept a queued capture from the current generation")
	}
}

func TestSuccessfulClearSkipsQueuedHistory(t *testing.T) {
	m := newTestApp(t, []delivery.Delivery{testDelivery("POST", "/original", `{}`)})
	m.cfg.OnClear = func() (uint64, error) { return 1, nil }
	sendKey(t, m, "x")
	sendKey(t, m, "y")

	m.Update(CaptureEvent{Delivery: testDelivery("POST", "/queued-before-clear", `{}`)})
	fresh := testDelivery("POST", "/queued-after-clear", `{}`)
	m.Update(CaptureEvent{Delivery: fresh, HistoryGeneration: 1})
	if len(m.items) != 1 || m.items[0].ID != fresh.ID {
		t.Fatal("only captures from the new generation should appear after clear")
	}
}
