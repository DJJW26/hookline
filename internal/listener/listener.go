// Package listener runs a local HTTP server that captures every inbound
// request, on any method and any path, as a delivery.Delivery.
package listener

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"hookline/internal/delivery"
)

// defaultMaxBodyBytes caps captured bodies when Config.MaxBodyBytes is unset.
const defaultMaxBodyBytes = 10 << 20

// Config controls how a Listener binds and responds.
type Config struct {
	// Host is the interface to bind; empty means 127.0.0.1.
	Host string
	// Port is the TCP port to bind; 0 means pick a free ephemeral port.
	Port int
	// Status is the HTTP status returned to callers; 0 means 200.
	Status int
	// MaxBodyBytes caps the captured body; values <= 0 mean 10 MiB.
	MaxBodyBytes int64
	// OnDelivery, when non-nil, is invoked from handler goroutines with each
	// captured delivery.
	OnDelivery func(delivery.Delivery)
}

// Listener is a running capture server. Stop it with Stop.
type Listener struct {
	srv        *http.Server
	host       string
	port       int
	onDelivery func(delivery.Delivery)
}

// Start binds and immediately serves cfg in background goroutines.
func Start(cfg Config) (*Listener, error) {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	status := cfg.Status
	if status < 100 || status > 999 {
		status = http.StatusOK
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return nil, fmt.Errorf("listener: listen on %s: %w", host, err)
	}
	port := cfg.Port
	if t, ok := ln.Addr().(*net.TCPAddr); ok {
		port = t.Port
	}
	l := &Listener{host: host, port: port, onDelivery: cfg.OnDelivery}
	l.srv = &http.Server{Handler: l.handler(status, maxBody)}
	go func() { _ = l.srv.Serve(ln) }()
	return l, nil
}

// Port returns the port actually bound, which may differ from Config.Port
// when an ephemeral port was requested.
func (l *Listener) Port() int { return l.port }

// URL returns the base URL the listener answers on, e.g. http://127.0.0.1:4040.
// Wildcard binds display as localhost — a URL clients can actually dial.
func (l *Listener) URL() string {
	host := l.host
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(l.port))
}

// Stop gracefully shuts the server down, waiting at most until ctx is done.
func (l *Listener) Stop(ctx context.Context) error {
	if err := l.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("listener: shutdown: %w", err)
	}
	return nil
}

// handler builds the capture handler used for every method and path.
func (l *Listener) handler(status int, maxBody int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		_ = r.Body.Close()
		truncated := int64(len(body)) > maxBody
		if truncated {
			body = body[:maxBody]
		}
		d := delivery.Delivery{
			ID:             delivery.NewID(),
			TS:             time.Now(),
			Method:         r.Method,
			Path:           r.URL.EscapedPath(),
			Query:          r.URL.RawQuery,
			Headers:        cloneHeaders(r.Header),
			Body:           body,
			SrcIP:          hostOnly(r.RemoteAddr),
			ResponseStatus: status,
			Truncated:      truncated,
		}
		h := w.Header()
		h.Set("Content-Type", "text/plain")
		h.Set("X-Hookline-ID", d.ID)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "hookline\n")
		if cb := l.onDelivery; cb != nil {
			cb(d)
		}
	})
}

// cloneHeaders deep-copies a header map so later request recycling cannot
// mutate the captured delivery.
func cloneHeaders(h http.Header) map[string][]string {
	m := make(map[string][]string, len(h))
	for k, vs := range h {
		c := make([]string, len(vs))
		copy(c, vs)
		m[k] = c
	}
	return m
}

// hostOnly strips the port from an addr such as 127.0.0.1:54321.
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
