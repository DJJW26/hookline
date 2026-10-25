package listener

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DJJW26/hookline/internal/delivery"
)

// Edge-case probes: unusual methods, encoded paths, duplicate headers,
// gzip bodies, chunked transfer, boundary body sizes, concurrency, and
// shutdown under load.

// collector gathers captures behind a mutex until closed.
type collector struct {
	mu   sync.Mutex
	list []delivery.Delivery
}

func (c *collector) add(d delivery.Delivery) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, d)
}

func (c *collector) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.list)
}

func (c *collector) get(i int) delivery.Delivery {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list[i]
}

func TestCaptureUnusualMethodsAndPaths(t *testing.T) {
	col := &collector{}
	ln, err := Start(Config{Port: 0, OnDelivery: col.add})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Stop(context.Background()) }()

	client := &http.Client{Timeout: 3 * time.Second}
	// Paths asserted here are the WIRE forms: capture is byte-faithful to
	// what arrived, so a UTF-8 path sent by a Go client arrives
	// percent-encoded (the client encodes it) and stays that way.
	paths := []string{
		"/a%20b/c+d",
		"/unicode/%D0%BF%D1%80%D0%B0%D0%B2%D0%B4%D0%B0/%E2%9C%A8",
		"/deep/" + strings.Repeat("x", 300),
	}
	for _, p := range paths {
		req, _ := http.NewRequest("PROPFIND", ln.URL()+p+"?k=1&k=2&flag&=", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PROPFIND %s: %v", p, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	deadline := time.Now().Add(3 * time.Second)
	for col.len() < len(paths) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if col.len() != len(paths) {
		t.Fatalf("captured %d of %d", col.len(), len(paths))
	}
	for i, p := range paths {
		d := col.get(i)
		if d.Path != p {
			t.Errorf("path %q captured as %q", p, d.Path)
		}
		if d.Method != "PROPFIND" {
			t.Errorf("method = %q", d.Method)
		}
		if d.Query != "k=1&k=2&flag&=" {
			t.Errorf("query = %q", d.Query)
		}
	}
}

func TestCaptureDuplicateAndCaseVariantHeaders(t *testing.T) {
	col := &collector{}
	ln, err := Start(Config{Port: 0, OnDelivery: col.add})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Stop(context.Background()) }()

	req, _ := http.NewRequest("POST", ln.URL()+"/h", strings.NewReader("x"))
	req.Header.Add("X-Dup", "one")
	req.Header.Add("x-dup", "two")
	req.Header.Set("X-Single", "s")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for col.len() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if col.len() != 1 {
		t.Fatal("not captured")
	}
	d := col.get(0)
	vals := d.Headers["X-Dup"]
	if len(vals) < 2 {
		t.Errorf("duplicate/case-variant header collapsed: %+v", d.Headers)
	}
	if d.Headers["X-Single"] == nil {
		t.Errorf("X-Single missing: %+v", d.Headers)
	}
}

func TestCaptureGzipBodyRoundTrip(t *testing.T) {
	col := &collector{}
	ln, err := Start(Config{Port: 0, OnDelivery: col.add})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Stop(context.Background()) }()

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte(`{"compressed":true}`))
	zw.Close()

	req, _ := http.NewRequest("POST", ln.URL()+"/gz", bytes.NewReader(gz.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for col.len() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if col.len() != 1 {
		t.Fatal("not captured")
	}
	d := col.get(0)
	// Fidelity is what matters: the exact compressed bytes for replay.
	if !bytes.Equal(d.Body, gz.Bytes()) {
		t.Errorf("gzip body not byte-identical: %d vs %d bytes", len(d.Body), gz.Len())
	}
	// Compressed bytes contain control characters → binary rendering.
	if d.Kind() != delivery.KindBinary {
		t.Logf("note: gzip body classified %q (binary expected)", d.Kind())
	}
}

func TestCaptureChunkedBody(t *testing.T) {
	col := &collector{}
	ln, err := Start(Config{Port: 0, OnDelivery: col.add})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Stop(context.Background()) }()

	pr, pw := io.Pipe()
	req, _ := http.NewRequest("POST", ln.URL()+"/chunked", pr)
	go func() {
		pw.Write([]byte(`{"chunked":`))
		pw.Write([]byte(`true}`))
		pw.Close()
	}()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for col.len() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if col.len() != 1 {
		t.Fatal("not captured")
	}
	if string(col.get(0).Body) != `{"chunked":true}` {
		t.Errorf("chunked body = %q", col.get(0).Body)
	}
}

func TestBodyBoundaryExactAndOneOver(t *testing.T) {
	const cap = int64(1024)
	col := &collector{}
	ln, err := Start(Config{Port: 0, MaxBodyBytes: cap, OnDelivery: col.add})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Stop(context.Background()) }()

	post := func(n int) {
		resp, err := http.Post(ln.URL()+"/b", "application/octet-stream",
			bytes.NewReader(bytes.Repeat([]byte{0x61}, n)))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	post(int(cap))     // exactly at cap
	post(int(cap) + 1) // one over
	deadline := time.Now().Add(3 * time.Second)
	for col.len() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if col.len() != 2 {
		t.Fatalf("captured %d of 2", col.len())
	}
	exact, over := col.get(0), col.get(1)
	if len(exact.Body) != int(cap) || exact.Truncated {
		t.Errorf("exact-cap body: len=%d truncated=%v", len(exact.Body), exact.Truncated)
	}
	if len(over.Body) != int(cap) || !over.Truncated {
		t.Errorf("over-cap body: len=%d truncated=%v", len(over.Body), over.Truncated)
	}
}

func TestConcurrentBurst200(t *testing.T) {
	col := &collector{}
	ln, err := Start(Config{Port: 0, OnDelivery: col.add})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Stop(context.Background()) }()

	const n = 200
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"i":%d}`, i)
			resp, err := http.Post(ln.URL()+"/burst", "application/json", strings.NewReader(body))
			if err != nil {
				t.Errorf("request %d: %v", i, err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}(i)
	}
	wg.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for col.len() < n && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if col.len() != n {
		t.Errorf("captured %d of %d under concurrent burst", col.len(), n)
	}
}

func TestStopUnderInFlightLoad(t *testing.T) {
	ln, err := Start(Config{Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for i := 0; i < 50; i++ {
			resp, err := http.Post(ln.URL()+"/drain", "application/json", strings.NewReader("{}"))
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ln.Stop(ctx); err != nil {
		t.Errorf("Stop under load: %v", err)
	}
}
