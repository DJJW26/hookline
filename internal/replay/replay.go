// Package replay re-sends captured deliveries to a new target and exports
// them as copy-pasteable cURL commands.
package replay

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DJJW26/hookline/internal/delivery"
)

// MaxResultBody caps how much of the replay target's response is kept for
// display: enough for stack traces and error JSON, never enough to hurt.
const MaxResultBody = 16 << 10

// Result reports the outcome of a replayed request.
type Result struct {
	// StatusCode is the HTTP status the target returned.
	StatusCode int
	// Duration is the total round-trip time of the replayed request.
	Duration time.Duration
	// Body is the head (≤ MaxResultBody) of the target's response — the
	// thing a developer actually needs to see when a replay fails.
	Body []byte
}

// hopHeaders are request headers that must never be forwarded on replay:
// they are hop-by-hop, routing, or owned by the transport.
var hopHeaders = []string{
	"Host",
	"Content-Length",
	"Connection",
	"Transfer-Encoding",
	"Te",
	"Upgrade",
	"Accept-Encoding",
}

// client sends replays with compression disabled (so the stripped
// Accept-Encoding header is not silently re-added) and never follows
// redirects: a replay must report what the target itself answered, not
// wherever a 302 happens to point.
var client = &http.Client{
	Transport:     &http.Transport{DisableCompression: true},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Do re-sends d to target (e.g. http://localhost:3000) using d's method,
// path, query, headers and body, honoring ctx cancellation. Headers that are
// hop-by-hop or transport-owned (Host, Content-Length, Connection,
// Transfer-Encoding, Te, Upgrade, Proxy-*, Accept-Encoding) are not
// forwarded. Transport-level failures are returned as errors; HTTP error
// statuses are reported via Result.StatusCode with a nil error.
func Do(ctx context.Context, target string, d delivery.Delivery) (Result, error) {
	full := strings.TrimSuffix(target, "/") + d.URL()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, d.Method, full, bytes.NewReader(d.Body))
	if err != nil {
		return Result{}, fmt.Errorf("replay: build %s %s: %w", d.Method, full, err)
	}
	for name, values := range d.Headers {
		if stripHeader(name) {
			continue
		}
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("replay: send %s %s: %w", d.Method, full, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxResultBody))
	return Result{StatusCode: resp.StatusCode, Duration: time.Since(start), Body: body}, nil
}

// stripHeader reports whether name belongs to the hop-by-hop or routing set
// that replay must not forward. Matching is case-insensitive.
func stripHeader(name string) bool {
	for _, h := range hopHeaders {
		if strings.EqualFold(name, h) {
			return true
		}
	}
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "proxy-")
}
