package main

import (
	"testing"

	"github.com/DJJW26/hookline/internal/delivery"
	"github.com/DJJW26/hookline/internal/store"
	"github.com/DJJW26/hookline/internal/ui"
)

func TestCaptureHistoryReportsFailedSaveEvenWhenQueueFills(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	h := &captureHistory{store: st, events: make(chan ui.CaptureEvent, 1)}
	d := delivery.Delivery{ID: "saved-to-ui-only", Method: "POST", Path: "/hooks"}
	h.record(d)
	h.record(delivery.Delivery{ID: "dropped"})
	select {
	case event := <-h.events:
		if event.Delivery.ID != d.ID || event.HistoryError == nil {
			t.Fatal("expected the original live delivery and its persistence error")
		}
	default:
		t.Fatal("failed history write should still enqueue a live event")
	}
	if failed, dropped := h.writeFailures.Load(), h.dropped.Load(); failed != 2 || dropped != 1 {
		t.Fatalf("failed saves = %d and bus drops = %d, want 2 and 1", failed, dropped)
	}
	if generation, err := h.clear(); generation != 0 || err == nil {
		t.Fatalf("failed clear returned generation %d, error %v", generation, err)
	}
}

func TestCaptureHistoryClearSeparatesQueuedEvents(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h := &captureHistory{store: st, events: make(chan ui.CaptureEvent, 2)}
	h.record(delivery.Delivery{ID: "old"})
	generation, err := h.clear()
	if err != nil {
		t.Fatal(err)
	}
	h.record(delivery.Delivery{ID: "new"})
	if len(h.events) != 2 {
		t.Fatal("both capture events should be queued")
	}
	old, fresh := <-h.events, <-h.events
	if old.HistoryGeneration >= generation || fresh.HistoryGeneration != generation {
		t.Fatalf("queued generations = %d, %d, clear returned %d", old.HistoryGeneration, fresh.HistoryGeneration, generation)
	}
	stored, err := st.LoadLast(2)
	if err != nil || len(stored) != 1 || stored[0].ID != fresh.Delivery.ID {
		t.Fatalf("stored history = %v, error %v", stored, err)
	}
}
