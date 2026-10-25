package store

import (
	"testing"
	"time"

	"github.com/DJJW26/hookline/internal/delivery"
)

// benchDelivery returns the fixed 1 KiB delivery the benchmarks persist,
// with a distinct ID per call so pre-seeded records stay unique.
func benchDelivery() delivery.Delivery {
	return delivery.Delivery{
		ID:      delivery.NewID(),
		TS:      time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
		Method:  "POST",
		Path:    "/bench",
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    make([]byte, 1024), // fixed 1 KiB, deterministic
	}
}

// BenchmarkAppend measures one flushed 1 KiB append into a fresh TempDir
// store — the price every captured delivery pays on the hot path.
func BenchmarkAppend(b *testing.B) {
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	defer s.Close()
	d := benchDelivery()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Append(d); err != nil {
			b.Fatalf("append: %v", err)
		}
	}
}

// BenchmarkLoadLast100 measures LoadLast(100) against a store pre-seeded
// with 1000 records — the startup cost of reading recent history.
func BenchmarkLoadLast100(b *testing.B) {
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	defer s.Close()
	for i := 0; i < 1000; i++ {
		if err := s.Append(benchDelivery()); err != nil {
			b.Fatalf("seed append %d: %v", i, err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := s.LoadLast(100)
		if err != nil {
			b.Fatalf("load: %v", err)
		}
		if len(got) != 100 {
			b.Fatalf("loaded %d records, want 100", len(got))
		}
	}
}
