// Adversarial wire-level probes for the listener: unusual methods, exotic
// paths/queries, header edge cases, body boundaries, chunked and gzip bodies,
// a 500-request burst, Stop during an in-flight request, and requests that
// net/http answers without ever reaching the capture handler.
package listener

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hookline/internal/delivery"
)

// probeCapture is a callback that records deliveries and counts them.
type probeCapture struct {
	mu   sync.Mutex
	got  []delivery.Delivery
	byID map[string]bool
}

func (c *probeCapture) add(d delivery.Delivery) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, d)
	if c.byID == nil {
		c.byID = map[string]bool{}
	}
	c.byID[d.ID] = true
}

func (c *probeCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func (c *probeCapture) findByPath(path string) (delivery.Delivery, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.got {
		if d.Path == path {
			return d, true
		}
	}
	return delivery.Delivery{}, false
}

// probeStart starts a listener whose captures land in cap.
func probeStart(t *testing.T, cap *probeCapture, cfg Config) *Listener {
	t.Helper()
	cfg.Port = 0
	cfg.OnDelivery = cap.add
	l, err := Start(cfg)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = l.Stop(ctx)
	})
	return l
}

// probeSendRaw dials the listener and writes tmpl (with the @HOST@ marker
// replaced by the listener's host:port) verbatim to the wire. It returns the
// status line and full response headers as one lowercase string.
func probeSendRaw(t *testing.T, l *Listener, tmpl string) (status string, headers string) {
	t.Helper()
	u, err := url.Parse(l.URL())
	if err != nil {
		t.Fatalf("parse listener URL: %v", err)
	}
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, strings.ReplaceAll(tmpl, "@HOST@", u.Host)); err != nil {
		t.Fatalf("write raw request: %v", err)
	}
	br := bufio.NewReader(conn)
	status, err = br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	var hb strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read header line: %v", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		hb.WriteString(line)
	}
	_, _ = io.Copy(io.Discard, br)
	return status, strings.ToLower(hb.String())
}

func TestProbeListenerUnusualMethodsPathsQueries(t *testing.T) {
	cap := &probeCapture{}
	l := probeStart(t, cap, Config{})

	long := "/"
	for i := 0; i < 500; i++ {
		long += "seg/"
	}
	cases := []struct {
		name      string
		tmpl      string
		wantPath  string
		wantQuery string
		wantMeth  string
	}{
		{
			name:     "lowercase method from raw client",
			tmpl:     "get /lower HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n",
			wantPath: "/lower", wantMeth: "get",
		},
		{
			name:     "PROPFIND",
			tmpl:     "PROPFIND /dav/depth HTTP/1.1\r\nHost: @HOST@\r\nDepth: 1\r\nConnection: close\r\n\r\n",
			wantPath: "/dav/depth", wantMeth: "PROPFIND",
		},
		{
			name:     "REPORT",
			tmpl:     "REPORT /cal/report HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n",
			wantPath: "/cal/report", wantMeth: "REPORT",
		},
		{
			name:     "HEAD",
			tmpl:     "HEAD /head HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n",
			wantPath: "/head", wantMeth: "HEAD",
		},
		{
			// %2F must stay encoded (it is not a path separator); unicode and
			// space escapes preserved byte-for-byte on the captured path.
			name:     "encoded slash unicode and space path",
			tmpl:     "GET /a%2Fb/caf%C3%A9/x%E2%9C%93/z%20z HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n",
			wantPath: "/a%2Fb/caf%C3%A9/x%E2%9C%93/z%20z", wantMeth: "GET",
		},
		{
			name:     "2000 char path",
			tmpl:     "GET " + long + " HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n",
			wantPath: long, wantMeth: "GET",
		},
		{
			name:     "query with empty key, dup keys, bare flag, empty value, percent",
			tmpl:     "GET /q?=1&a=1&a=2&flag&b=&x=%20;y HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n",
			wantPath: "/q", wantQuery: "=1&a=1&a=2&flag&b=&x=%20;y", wantMeth: "GET",
		},
		{
			// A bare trailing "?" produces an empty RawQuery, recorded the
			// same as no query at all (cosmetic wart, asserted as-is).
			name:     "empty query marker",
			tmpl:     "GET /emptyq? HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n",
			wantPath: "/emptyq", wantQuery: "", wantMeth: "GET",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := probeSendRaw(t, l, tc.tmpl)
			if !strings.Contains(status, "200") {
				t.Fatalf("status line = %q, want 200", status)
			}
			waitFor(t, 5*time.Second, "capture for "+tc.wantPath, func() bool {
				_, ok := cap.findByPath(tc.wantPath)
				return ok
			})
			d, _ := cap.findByPath(tc.wantPath)
			if d.Method != tc.wantMeth {
				t.Errorf("method = %q, want %q", d.Method, tc.wantMeth)
			}
			if d.Query != tc.wantQuery {
				t.Errorf("query = %q, want %q", d.Query, tc.wantQuery)
			}
			if d.Path != tc.wantPath {
				t.Errorf("path = %q, want %q", d.Path, tc.wantPath)
			}
		})
	}
}

func TestProbeListenerHeaderEdges(t *testing.T) {
	cap := &probeCapture{}
	l := probeStart(t, cap, Config{})

	status, _ := probeSendRaw(t, l,
		"GET /hdr HTTP/1.1\r\nHost: @HOST@\r\n"+
			"X-Dup: one\r\nx-dup: two\r\nX-DUP: three\r\n"+
			"X-Weird: a\tb \xff\xfe\r\n"+
			"X-Pad:  padded  \r\n"+
			"Connection: close\r\n\r\n")
	if !strings.Contains(status, "200") {
		t.Fatalf("status line = %q, want 200", status)
	}
	waitFor(t, 5*time.Second, "capture /hdr", func() bool {
		_, ok := cap.findByPath("/hdr")
		return ok
	})
	d, _ := cap.findByPath("/hdr")

	// Case variants merge under the canonical key, values in arrival order.
	dup, ok := d.Headers["X-Dup"]
	if !ok {
		t.Fatalf("X-Dup missing from captured headers: %v", d.Headers)
	}
	if len(dup) != 3 || dup[0] != "one" || dup[1] != "two" || dup[2] != "three" {
		t.Errorf("X-Dup values = %v, want [one two three]", dup)
	}
	// Tab and obs-text (high-bit) bytes survive verbatim.
	if w := headerValue(d.Headers, "X-Weird"); w != "a\tb \xff\xfe" {
		t.Errorf("X-Weird = %q, want %q", w, "a\tb \xff\xfe")
	}
	// Leading/trailing OWS is trimmed by net/http before capture.
	if p := headerValue(d.Headers, "X-Pad"); p != "padded" {
		t.Errorf("X-Pad = %q, want %q", p, "padded")
	}

	// A NUL inside a header value is rejected by net/http with a 400 before
	// the capture handler runs; nothing is recorded, nothing panics.
	before := cap.count()
	status, _ = probeSendRaw(t, l, "GET /badhdr HTTP/1.1\r\nHost: @HOST@\r\nX-Bad: a\x00b\r\nConnection: close\r\n\r\n")
	if !strings.Contains(status, "400") {
		t.Errorf("NUL header status = %q, want 400", status)
	}
	time.Sleep(150 * time.Millisecond)
	if got := cap.count(); got != before {
		t.Errorf("NUL header request was captured (%d -> %d deliveries)", before, got)
	}
}

func TestProbeListenerBodyBoundaries(t *testing.T) {
	cap := &probeCapture{}
	l := probeStart(t, cap, Config{MaxBodyBytes: 16})

	cases := []struct {
		name          string
		bodyLen       int
		wantLen       int
		wantTruncated bool
	}{
		{name: "empty body POST", bodyLen: 0, wantLen: 0},
		{name: "body exactly at cap", bodyLen: 16, wantLen: 16},
		{name: "body one past cap", bodyLen: 17, wantLen: 16, wantTruncated: true},
		{name: "body far past cap", bodyLen: 100, wantLen: 16, wantTruncated: true},
	}
	client := &http.Client{}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := "/body/" + strconv.Itoa(i)
			body := bytes.Repeat([]byte{'x'}, tc.bodyLen)
			req, err := http.NewRequest("POST", l.URL()+path, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Header.Set("Content-Type", "text/plain")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			_ = resp.Body.Close()
			waitFor(t, 5*time.Second, "capture "+path, func() bool {
				_, ok := cap.findByPath(path)
				return ok
			})
			d, _ := cap.findByPath(path)
			if len(d.Body) != tc.wantLen {
				t.Errorf("body len = %d, want %d", len(d.Body), tc.wantLen)
			}
			if d.Truncated != tc.wantTruncated {
				t.Errorf("Truncated = %v, want %v", d.Truncated, tc.wantTruncated)
			}
			if !bytes.Equal(d.Body, body[:tc.wantLen]) {
				t.Errorf("stored body is not the sent prefix")
			}
		})
	}
	client.CloseIdleConnections()
}

func TestProbeListenerChunkedAndGzip(t *testing.T) {
	cap := &probeCapture{}
	l := probeStart(t, cap, Config{})

	// Chunked upload with a chunk extension: the captured body must be the
	// decoded payload exactly.
	probeSendRaw(t, l, "POST /chunked HTTP/1.1\r\nHost: @HOST@\r\n"+
		"Transfer-Encoding: chunked\r\nConnection: close\r\n\r\n"+
		"5\r\nhello\r\n6;ext=1\r\n world\r\n0\r\n\r\n")
	waitFor(t, 5*time.Second, "capture /chunked", func() bool {
		_, ok := cap.findByPath("/chunked")
		return ok
	})
	d, _ := cap.findByPath("/chunked")
	if string(d.Body) != "hello world" {
		t.Errorf("chunked body = %q, want %q", d.Body, "hello world")
	}
	if d.Truncated {
		t.Error("chunked body flagged truncated")
	}

	// Gzip Content-Encoding: the listener must NOT decompress; raw bytes and
	// the Content-Encoding header are captured so replay stays byte-faithful.
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(`{"compressed":true}`)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	client := &http.Client{}
	req, err := http.NewRequest("POST", l.URL()+"/gz", bytes.NewReader(gz.Bytes()))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send gzip body: %v", err)
	}
	_ = resp.Body.Close()
	client.CloseIdleConnections()
	waitFor(t, 5*time.Second, "capture /gz", func() bool {
		_, ok := cap.findByPath("/gz")
		return ok
	})
	d, _ = cap.findByPath("/gz")
	if !bytes.Equal(d.Body, gz.Bytes()) {
		t.Errorf("gzip body not preserved raw: got %d bytes, want %d", len(d.Body), gz.Len())
	}
	if ce := headerValue(d.Headers, "Content-Encoding"); ce != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", ce)
	}
	// Current classification: a gzip payload renders as binary (magic 0x1f
	// is a control byte). Assert non-panic and the current verdict.
	if k := d.Kind(); k != delivery.KindBinary {
		t.Errorf("gzip body Kind() = %q, want %q", k, delivery.KindBinary)
	}
}

func TestProbeListenerBurst500(t *testing.T) {
	cap := &probeCapture{}
	l := probeStart(t, cap, Config{MaxBodyBytes: 1 << 20})

	const n = 500
	// Fire in waves of 50 concurrent requests: 500 simultaneously-dialed
	// loopback connections can trip Windows SYN handling — an OS limit,
	// not a listener limit. Waves still prove volume plus concurrency.
	const waveSize = 50
	client := &http.Client{}
	var wg sync.WaitGroup
	for base := 0; base < n; base += waveSize {
		for i := base; i < base+waveSize; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				body := fmt.Sprintf(`{"i":%d}`, i)
				req, err := http.NewRequest("POST", l.URL()+"/burst/"+strconv.Itoa(i), strings.NewReader(body))
				if err != nil {
					t.Errorf("build request %d: %v", i, err)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					t.Errorf("request %d: %v", i, err)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}(i)
		}
		wg.Wait()
	}
	client.CloseIdleConnections()

	waitFor(t, 30*time.Second, "500 captured deliveries", func() bool {
		return cap.count() == n
	})
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.got) != n {
		t.Fatalf("captured %d deliveries, want %d", len(cap.got), n)
	}
	ids := make(map[string]bool, n)
	paths := make(map[string]bool, n)
	for _, d := range cap.got {
		if ids[d.ID] {
			t.Fatalf("duplicate ID %q in burst", d.ID)
		}
		ids[d.ID] = true
		paths[d.Path] = true
	}
	if len(paths) != n {
		t.Errorf("unique burst paths = %d, want %d", len(paths), n)
	}
}

func TestProbeListenerStopWhileRequestInFlight(t *testing.T) {
	cap := &probeCapture{}
	l := probeStart(t, cap, Config{})

	// Open a raw connection and send complete headers with a body never
	// arriving: the handler is in flight, blocked reading the body.
	u, _ := url.Parse(l.URL())
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := io.WriteString(conn, "POST /slow HTTP/1.1\r\nHost: "+u.Host+"\r\nContent-Length: 5\r\n\r\n"); err != nil {
		t.Fatalf("write partial request: %v", err)
	}

	// Graceful shutdown with a short deadline. Documented behavior: the
	// torn connection is cut (possibly before ctx expiry) and — because a
	// half-delivered body is noise — the listener records NOTHING for it.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer shortCancel()
	_ = l.Stop(shortCtx)

	// The aborted body may surface as a read error; the handler must not
	// record a partial delivery.
	_, _ = conn.Write([]byte("hello"))
	time.Sleep(300 * time.Millisecond)
	if got := cap.count(); got != 0 {
		t.Errorf("torn request was captured (%d deliveries), want 0", got)
	}

	// A second Stop drains cleanly regardless.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := l.Stop(ctx); err != nil {
		t.Errorf("second Stop error: %v", err)
	}
}

// TestProbeListenerUncapturableRequests documents two request shapes that
// net/http answers without ever invoking the capture handler: `OPTIONS *`
// (handled by net/http's global options handler) and paths with invalid
// percent-escapes (rejected with 400 by the URL parser). The listener cannot
// record these; asserted here so a behavior change is noticed.
func TestProbeListenerUncapturableRequests(t *testing.T) {
	cap := &probeCapture{}
	l := probeStart(t, cap, Config{})

	status, headers := probeSendRaw(t, l, "OPTIONS * HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n")
	if !strings.Contains(status, "200") {
		t.Errorf("OPTIONS * status = %q, want 200", status)
	}
	if strings.Contains(headers, "x-hookline-id") {
		t.Errorf("OPTIONS * unexpectedly reached the capture handler: %q", headers)
	}

	status, _ = probeSendRaw(t, l, "GET /p%zz HTTP/1.1\r\nHost: @HOST@\r\nConnection: close\r\n\r\n")
	if !strings.Contains(status, "400") {
		t.Errorf("invalid escape status = %q, want 400", status)
	}

	time.Sleep(200 * time.Millisecond)
	if got := cap.count(); got != 0 {
		t.Errorf("captured %d deliveries from uncapturable requests, want 0", got)
	}
}
