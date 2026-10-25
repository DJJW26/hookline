package listener

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DJJW26/hookline/internal/delivery"
)

// benchBody is the fixed 1 KiB JSON payload every benchmark capture sends.
var benchBody = []byte(`{"pad":"` + strings.Repeat("a", 1014) + `"}`)

// BenchmarkCapture posts a fixed 1 KiB JSON body to a running listener and
// measures the full capture path: read, classify, respond, OnDelivery. The
// collector counts atomically — the callback fires from handler goroutines,
// after the response is already on the wire.
func BenchmarkCapture(b *testing.B) {
	var captured atomic.Int64
	ln, err := Start(Config{
		Port: 0, // ephemeral: never collides with a dev server
		OnDelivery: func(delivery.Delivery) {
			captured.Add(1)
		},
	})
	if err != nil {
		b.Fatalf("listener: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ln.Stop(ctx); err != nil {
			b.Errorf("stop: %v", err)
		}
	}()

	url := ln.URL() + "/bench"
	client := &http.Client{Timeout: 10 * time.Second}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Post(url, "application/json", bytes.NewReader(benchBody))
		if err != nil {
			b.Fatalf("post: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		// Drain so the connection is reusable for the next iteration.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	b.StopTimer()
	// The callback fires after the response is written, so the last one may
	// still be in flight; give it a moment before asserting the count.
	deadline := time.Now().Add(5 * time.Second)
	for captured.Load() != int64(b.N) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := captured.Load(); got != int64(b.N) {
		b.Errorf("captured %d deliveries, want %d", got, b.N)
	}
}
