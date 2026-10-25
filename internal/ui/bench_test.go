package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/DJJW26/hookline/internal/delivery"
	"github.com/DJJW26/hookline/internal/style"
)

// Benchmarks for the render loop. The ASCII profile is forced here the same
// way TestMain forces it for tests (bench_test.go runs in the same test
// binary), so View output and allocation counts never depend on the runner's
// terminal.

// mixedDelivery returns the i-th of a deterministic set of deliveries that
// cycles through the method colours and every body renderer the frame has.
func mixedDelivery(i int) delivery.Delivery {
	methods := []string{"POST", "GET", "PUT", "PATCH", "DELETE"}
	d := delivery.Delivery{
		ID:             delivery.NewID(),
		TS:             time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC),
		Method:         methods[i%len(methods)],
		Path:           fmt.Sprintf("/hooks/bench/%03d", i),
		Headers:        map[string][]string{"Content-Type": {"application/json"}},
		ResponseStatus: 200,
	}
	switch i % 4 {
	case 0:
		d.Body = []byte(fmt.Sprintf(`{"event":"bench","seq":%d,"ok":true}`, i))
	case 1:
		d.Headers["Content-Type"] = []string{"application/x-www-form-urlencoded"}
		d.Body = []byte(fmt.Sprintf("a=1&b=two+words&seq=%d", i))
	case 2:
		d.Body = nil // empty body state
	case 3:
		d.Headers["Content-Type"] = []string{"text/plain"}
		d.Body = []byte(fmt.Sprintf("plain payload %d — %s", i, strings.Repeat("pad ", 24)))
	}
	return d
}

// BenchmarkIngestEvict feeds 1 MiB deliveries through Update against a
// session pre-filled to exactly the maxSessionBytes cap, so every iteration
// pays the full ingest path: prepend, counter add, unread mark, and one real
// eviction (counter subtract, slice truncate, per-delivery cache cleanup).
func BenchmarkIngestEvict(b *testing.B) {
	lipgloss.SetColorProfile(termenv.Ascii)
	theme, err := style.Lookup("dark")
	if err != nil {
		b.Fatalf("lookup dark theme: %v", err)
	}
	events := make(chan delivery.Delivery, 8)
	m := New(Config{Version: "bench", Events: events}, style.New(theme), theme)
	body := make([]byte, 1<<20) // 1 MiB: 256 of these reach the cap
	seq := 0
	deliver := func() {
		model, _ := m.Update(deliveryMsg(delivery.Delivery{
			ID:             delivery.NewID(),
			TS:             time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC),
			Method:         "POST",
			Path:           fmt.Sprintf("/ingest/%d", seq),
			Body:           body,
			ResponseStatus: 200,
		}))
		m = model.(*Model)
		seq++
	}
	for i := 0; i < 256; i++ { // pre-fill outside the timer: counter == cap
		deliver()
	}
	if m.sessionBytes != maxSessionBytes {
		b.Fatalf("setup: counter = %d, want %d", m.sessionBytes, maxSessionBytes)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		deliver()
	}
	b.StopTimer()
	// Steady state: every iteration ingested one and evicted one.
	if len(m.items) != 256 {
		b.Errorf("items = %d, want 256 (one eviction per ingest)", len(m.items))
	}
	if m.sessionBytes != maxSessionBytes {
		b.Errorf("counter = %d, want %d (eviction must keep the cap)", m.sessionBytes, maxSessionBytes)
	}
}

// BenchmarkViewFrame renders one full frame of a model holding 100 mixed
// deliveries at 120x30 — the steady-state cost of the ~60fps render loop.
func BenchmarkViewFrame(b *testing.B) {
	lipgloss.SetColorProfile(termenv.Ascii)
	theme, err := style.Lookup("dark")
	if err != nil {
		b.Fatalf("lookup dark theme: %v", err)
	}
	events := make(chan delivery.Delivery, 8)
	m := New(Config{
		Version:   "bench",
		ListenURL: "http://127.0.0.1:4040",
		Events:    events,
	}, style.New(theme), theme)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = model.(*Model)
	for i := 0; i < 100; i++ {
		model, _ = m.Update(deliveryMsg(mixedDelivery(i)))
		m = model.(*Model)
	}
	if len(m.items) != 100 {
		b.Fatalf("items = %d, want 100", len(m.items))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if v := m.View(); v == "" {
			b.Fatal("empty view")
		}
	}
}
