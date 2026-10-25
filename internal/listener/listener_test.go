package listener

import (
	"bytes"
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

// reqSpec describes one request the test fires and what must be captured.
type reqSpec struct {
	method      string
	path        string // no query
	query       string // raw query as it should be captured
	body        []byte
	contentType string
	trace       string // unique X-Trace header value
}

// respInfo records what the listener answered to one request.
type respInfo struct {
	status int
	hookID string
	body   string
	ct     string
}

// waitFor polls cond until it holds or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, desc)
}

func TestListenerConcurrentCapture(t *testing.T) {
	var mu sync.Mutex
	var deliveries []delivery.Delivery

	cfg := Config{
		Port:         0,
		Status:       200,
		MaxBodyBytes: 1 << 20,
		OnDelivery: func(d delivery.Delivery) {
			mu.Lock()
			defer mu.Unlock()
			deliveries = append(deliveries, d)
		},
	}
	l, err := Start(cfg)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := l.Stop(ctx); err != nil {
			t.Errorf("Stop error: %v", err)
		}
	}()
	if l.Port() <= 0 {
		t.Fatalf("Port() = %d, want > 0", l.Port())
	}
	if !strings.HasPrefix(l.URL(), "http://127.0.0.1:") {
		t.Errorf("URL() = %q, want http://127.0.0.1:<port>", l.URL())
	}

	// Build a mix of 50 specs: JSON, form, binary with NUL bytes, GET+query.
	specs := make([]reqSpec, 0, 50)
	for i := 0; i < 50; i++ {
		sp := reqSpec{
			path:   fmt.Sprintf("/hook/%d", i),
			trace:  fmt.Sprintf("trace-%d", i),
			method: "POST",
		}
		switch {
		case i < 20:
			sp.body = []byte(fmt.Sprintf(`{"n":%d,"msg":"hello json"}`, i))
			sp.contentType = "application/json"
		case i < 30:
			sp.body = []byte(fmt.Sprintf("a=1&i=%d&note=hello+form", i))
			sp.contentType = "application/x-www-form-urlencoded"
		case i < 40:
			sp.body = append([]byte{0x00, 0x01, 0x00, 0xff, 0xfe}, byte(i), 0x00)
			sp.contentType = "application/octet-stream"
		default:
			sp.method = "GET"
			sp.query = fmt.Sprintf("x=%d&q=hello+world", i)
		}
		specs = append(specs, sp)
	}

	var resMu sync.Mutex
	responses := make(map[string]respInfo, len(specs)) // keyed by path
	client := &http.Client{}
	var wg sync.WaitGroup
	for _, sp := range specs {
		wg.Add(1)
		go func(sp reqSpec) {
			defer wg.Done()
			url := l.URL() + sp.path
			if sp.query != "" {
				url += "?" + sp.query
			}
			req, err := http.NewRequest(sp.method, url, bytes.NewReader(sp.body))
			if err != nil {
				t.Errorf("build %s %s: %v", sp.method, url, err)
				return
			}
			req.Header.Set("X-Trace", sp.trace)
			if sp.contentType != "" {
				req.Header.Set("Content-Type", sp.contentType)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("%s %s: %v", sp.method, url, err)
				return
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Errorf("read response for %s: %v", sp.path, err)
				return
			}
			resMu.Lock()
			responses[sp.path] = respInfo{
				status: resp.StatusCode,
				hookID: resp.Header.Get("X-Hookline-ID"),
				body:   string(body),
				ct:     resp.Header.Get("Content-Type"),
			}
			resMu.Unlock()
		}(sp)
	}
	wg.Wait()
	// Release keep-alive connections so graceful shutdown has nothing to wait for.
	client.CloseIdleConnections()

	waitFor(t, 10*time.Second, "50 deliveries", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(deliveries) == 50
	})

	mu.Lock()
	defer mu.Unlock()
	if len(deliveries) != 50 {
		t.Fatalf("captured %d deliveries, want 50", len(deliveries))
	}
	byPath := make(map[string]delivery.Delivery, len(deliveries))
	for _, d := range deliveries {
		byPath[d.Path] = d
	}
	seenIDs := make(map[string]bool, len(deliveries))
	for _, d := range deliveries {
		seenIDs[d.ID] = true
	}
	for _, sp := range specs {
		d, ok := byPath[sp.path]
		if !ok {
			t.Errorf("no delivery captured for path %s", sp.path)
			continue
		}
		if d.Method != sp.method {
			t.Errorf("%s: method = %q, want %q", sp.path, d.Method, sp.method)
		}
		if d.Query != sp.query {
			t.Errorf("%s: query = %q, want %q", sp.path, d.Query, sp.query)
		}
		if !bytes.Equal(d.Body, sp.body) {
			t.Errorf("%s: body not byte-identical: got %q, want %q", sp.path, d.Body, sp.body)
		}
		if got := headerValue(d.Headers, "X-Trace"); got != sp.trace {
			t.Errorf("%s: X-Trace header = %q, want %q", sp.path, got, sp.trace)
		}
		if sp.contentType != "" {
			if got := headerValue(d.Headers, "Content-Type"); got != sp.contentType {
				t.Errorf("%s: Content-Type header = %q, want %q", sp.path, got, sp.contentType)
			}
		}
		if d.ID == "" || d.TS.IsZero() {
			t.Errorf("%s: delivery has empty ID or TS", sp.path)
		}
		if d.Truncated {
			t.Errorf("%s: Truncated = true, want false", sp.path)
		}
		if d.SrcIP != "127.0.0.1" {
			t.Errorf("%s: SrcIP = %q, want 127.0.0.1", sp.path, d.SrcIP)
		}
		if d.ResponseStatus != 200 {
			t.Errorf("%s: ResponseStatus = %d, want 200", sp.path, d.ResponseStatus)
		}
	}
	for path, ri := range responses {
		if ri.status != 200 {
			t.Errorf("%s: response status = %d, want 200", path, ri.status)
		}
		if ri.body != "hookline\n" {
			t.Errorf("%s: response body = %q, want %q", path, ri.body, "hookline\n")
		}
		if ri.ct != "text/plain" {
			t.Errorf("%s: response Content-Type = %q, want text/plain", path, ri.ct)
		}
		if ri.hookID == "" {
			t.Errorf("%s: response missing X-Hookline-ID", path)
			continue
		}
		if !seenIDs[ri.hookID] {
			t.Errorf("%s: X-Hookline-ID %q not among captured delivery IDs", path, ri.hookID)
		}
	}
}

func TestListenerTruncatesBodyAndCustomStatus(t *testing.T) {
	var mu sync.Mutex
	var got []delivery.Delivery
	l, err := Start(Config{
		Port:         0,
		Status:       201,
		MaxBodyBytes: 1024,
		OnDelivery: func(d delivery.Delivery) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, d)
		},
	})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		if err := l.Stop(ctx); err != nil {
			t.Errorf("Stop error: %v", err)
		}
	}()

	body := bytes.Repeat([]byte("abcd"), 1024) // 4096 bytes
	req, err := http.NewRequest("POST", l.URL()+"/big", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post oversized body: %v", err)
	}
	_ = resp.Body.Close()
	client.CloseIdleConnections()
	if resp.StatusCode != 201 {
		t.Errorf("response status = %d, want 201", resp.StatusCode)
	}
	if resp.Header.Get("X-Hookline-ID") == "" {
		t.Error("response missing X-Hookline-ID header")
	}

	waitFor(t, 5*time.Second, "delivery of oversized body", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})
	mu.Lock()
	defer mu.Unlock()
	d := got[0]
	if !d.Truncated {
		t.Error("Truncated = false, want true")
	}
	if len(d.Body) != 1024 {
		t.Errorf("stored body len = %d, want 1024", len(d.Body))
	}
	if !bytes.Equal(d.Body, body[:1024]) {
		t.Error("stored body is not the prefix of the sent body")
	}
	if d.ResponseStatus != 201 {
		t.Errorf("ResponseStatus = %d, want 201", d.ResponseStatus)
	}
}

func TestListenerNilCallbackStillCaptures(t *testing.T) {
	l, err := Start(Config{Port: 0}) // OnDelivery nil, defaults elsewhere
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		if err := l.Stop(ctx); err != nil {
			t.Errorf("Stop error: %v", err)
		}
	}()
	client := &http.Client{}
	resp, err := client.Get(l.URL() + "/ping?a=1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	client.CloseIdleConnections()
	if resp.StatusCode != 200 {
		t.Errorf("default status = %d, want 200", resp.StatusCode)
	}
	if string(body) != "hookline\n" {
		t.Errorf("default response body = %q, want %q", body, "hookline\n")
	}
	if resp.Header.Get("X-Hookline-ID") == "" {
		t.Error("response missing X-Hookline-ID header")
	}
}

// headerValue returns the first value for a case-insensitive header name.
func headerValue(h map[string][]string, name string) string {
	for k, vs := range h {
		if strings.EqualFold(k, name) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}
