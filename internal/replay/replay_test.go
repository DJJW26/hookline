package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"hookline/internal/delivery"
)

// capturedReq snapshots everything the test server received.
type capturedReq struct {
	method  string
	path    string
	rawQ    string
	host    string
	headers http.Header
	body    []byte
}

// captureServer returns a test server that snapshots the last request.
func captureServer(t *testing.T) (*httptest.Server, *capturedReq) {
	t.Helper()
	var mu sync.Mutex
	got := &capturedReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("test server read body: %v", err)
		}
		h := make(http.Header, len(r.Header))
		for k, vs := range r.Header {
			h[k] = append([]string(nil), vs...)
		}
		mu.Lock()
		got.method, got.path, got.rawQ, got.host, got.headers, got.body = r.Method, r.URL.Path, r.URL.RawQuery, r.Host, h, body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestDoForwardsRequest(t *testing.T) {
	srv, got := captureServer(t)
	body := []byte(`{"msg":"it's alive","n":1}`)
	d := delivery.Delivery{
		ID:     "d-1",
		Method: "POST",
		Path:   "/hook/path",
		Query:  "a=1&b=two",
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
			"X-Test":       {"hello"},
			// Everything below must be stripped, never forwarded.
			"Host":                {"hookline.example"},
			"Content-Length":      {"999999"},
			"Connection":          {"keep-alive"},
			"Transfer-Encoding":   {"chunked"},
			"Te":                  {"trailers"},
			"Upgrade":             {"websocket"},
			"Proxy-Authorization": {"Basic zzz"},
			"Proxy-Foo":           {"bar"},
			"Accept-Encoding":     {"gzip"},
		},
		Body: body,
	}
	res, err := Do(context.Background(), srv.URL, d)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", res.StatusCode)
	}
	if res.Duration < 0 {
		t.Errorf("Duration = %v, want >= 0", res.Duration)
	}

	if got.method != d.Method {
		t.Errorf("forwarded method = %q, want %q", got.method, d.Method)
	}
	if got.path != d.Path {
		t.Errorf("forwarded path = %q, want %q", got.path, d.Path)
	}
	if got.rawQ != d.Query {
		t.Errorf("forwarded query = %q, want %q", got.rawQ, d.Query)
	}
	if !bytes.Equal(got.body, body) {
		t.Errorf("forwarded body not byte-identical: got %q, want %q", got.body, body)
	}
	if v := got.headers.Get("X-Test"); v != "hello" {
		t.Errorf("X-Test = %q, want %q", v, "hello")
	}
	if v := got.headers.Get("Content-Type"); v != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", v)
	}
	for _, h := range []string{"Connection", "Accept-Encoding", "Transfer-Encoding", "Te", "Upgrade", "Proxy-Authorization", "Proxy-Foo"} {
		if v := got.headers.Get(h); v != "" {
			t.Errorf("stripped header %s was forwarded as %q", h, v)
		}
	}
	if got.host == "hookline.example" {
		t.Errorf("original Host header forwarded: r.Host = %q", got.host)
	}
	// The transport may set its own correct Content-Length; the stale value
	// from the delivery must never survive.
	if cl := got.headers.Get("Content-Length"); cl != "" && cl != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %q, want %q or absent", cl, strconv.Itoa(len(body)))
	}
}

func TestDoReturnsHTTPStatusWithoutError(t *testing.T) {
	for _, code := range []int{200, 201, 400, 404, 418, 500, 503} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		d := delivery.Delivery{ID: "d", Method: "POST", Path: "/x", Body: []byte("ping")}
		res, err := Do(context.Background(), srv.URL, d)
		srv.Close()
		if err != nil {
			t.Errorf("status %d: Do error: %v", code, err)
			continue
		}
		if res.StatusCode != code {
			t.Errorf("status %d: StatusCode = %d", code, res.StatusCode)
		}
	}
}

func TestDoHonorsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := delivery.Delivery{ID: "d", Method: "GET", Path: "/x"}
	if _, err := Do(ctx, srv.URL, d); err == nil {
		t.Fatal("Do with canceled context returned nil error")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled in chain", err)
	}
}

func TestToCurl(t *testing.T) {
	jsonD := delivery.Delivery{
		Method: "POST",
		Path:   "/hook",
		Query:  "x=1",
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
			"X-Api-Key":    {"k-123"},
		},
		Body: []byte(`{"a":1}`),
	}
	binaryD := delivery.Delivery{
		Method: "POST",
		Path:   "/bin",
		Headers: map[string][]string{
			"Content-Type": {"application/octet-stream"},
		},
		Body: []byte{0x00, 0x01, 0xff},
	}
	tests := []struct {
		name        string
		baseURL     string
		d           delivery.Delivery
		contains    []string
		notContains []string
	}{
		{
			name:    "post json with headers and body",
			baseURL: "http://localhost:3000",
			d:       jsonD,
			contains: []string{
				"-X POST",
				"'http://localhost:3000/hook?x=1'",
				"-H 'Content-Type: application/json'",
				"-H 'X-Api-Key: k-123'",
				"--data-binary '{\"a\":1}'",
			},
		},
		{
			name:    "base url with trailing slash",
			baseURL: "http://localhost:3000/",
			d:       jsonD,
			contains: []string{
				"'http://localhost:3000/hook?x=1'",
			},
			notContains: []string{"3000//hook"},
		},
		{
			name:    "body with single quote is shell-escaped",
			baseURL: "http://localhost:3000",
			d: delivery.Delivery{
				Method: "POST",
				Path:   "/q",
				Body:   []byte("it's"),
			},
			contains: []string{
				"--data-binary 'it'\\''s'",
			},
			notContains: []string{"--data-binary 'it's'"},
		},
		{
			name:    "empty body omits data flag",
			baseURL: "http://localhost:3000",
			d: delivery.Delivery{
				Method: "GET",
				Path:   "/ping",
			},
			contains: []string{
				"-X GET",
				"'http://localhost:3000/ping'",
			},
			notContains: []string{"--data-binary"},
		},
		{
			name:    "binary body gets trailing comment instead of inline data",
			baseURL: "http://localhost:3000",
			d:       binaryD,
			contains: []string{
				"  # binary body (3 bytes): save and pass --data-binary @file",
			},
			notContains: []string{"--data-binary '"},
		},
		{
			name:    "multiple header values become separate flags",
			baseURL: "http://localhost:3000",
			d: delivery.Delivery{
				Method: "POST",
				Path:   "/m",
				Headers: map[string][]string{
					"X-Multi": {"a", "b"},
				},
				Body: []byte("x"),
			},
			contains: []string{
				"-H 'X-Multi: a'",
				"-H 'X-Multi: b'",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := ToCurl(tc.baseURL, tc.d)
			if strings.Contains(out, "\n") {
				t.Errorf("output must be one line, got: %s", out)
			}
			for _, want := range tc.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
			for _, ban := range tc.notContains {
				if strings.Contains(out, ban) {
					t.Errorf("output must not contain %q:\n%s", ban, out)
				}
			}
		})
	}
}
