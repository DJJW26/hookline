// Package delivery defines the Delivery record: the single shared type that
// flows between the listener, store, replay and UI layers. Core packages
// depend only on this type, never on each other's internals.
package delivery

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Kind classifies a delivery body so the UI can pick a renderer.
type Kind string

// Body kinds, in order of detection preference.
const (
	KindEmpty  Kind = ""     // no body
	KindJSON   Kind = "json" // JSON body (declared or sniffed)
	KindForm   Kind = "form" // urlencoded form body
	KindBinary Kind = "bin"  // non-printable body
	KindText   Kind = "text" // anything else
)

// A Delivery is one captured inbound HTTP request, exactly as received.
type Delivery struct {
	ID      string              `json:"id"`
	TS      time.Time           `json:"ts"`
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Query   string              `json:"query,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	// Body is encoded as base64 on disk and on the wire (encoding/json's
	// native []byte handling), so binary payloads round-trip safely.
	Body           []byte `json:"body_b64"`
	SrcIP          string `json:"src_ip,omitempty"`
	ResponseStatus int    `json:"response_status"`
	Truncated      bool   `json:"truncated,omitempty"`
}

// NewID returns a random, sortable identifier for a delivery. It is prefixed
// with the unix millisecond timestamp so stored files stay ordered.
func NewID() string {
	var b [6]byte
	ts := time.Now().UnixMilli()
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing means the system is broken; fall back to a
		// time-only ID rather than panicking inside a request handler.
		return fmt.Sprintf("%013d", ts)
	}
	return fmt.Sprintf("%013d-%s", ts, hex.EncodeToString(b[:]))
}

// URL returns path plus query, the part of the request URL after the host.
func (d Delivery) URL() string {
	if d.Query == "" {
		return d.Path
	}
	return d.Path + "?" + d.Query
}

// Size returns the body size in bytes.
func (d Delivery) Size() int { return len(d.Body) }

// ContentType returns the canonical Content-Type header value, or "".
func (d Delivery) ContentType() string {
	for k, vs := range d.Headers {
		if strings.EqualFold(k, "Content-Type") && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

// Kind reports how the body should be rendered.
func (d Delivery) Kind() Kind {
	if len(d.Body) == 0 {
		return KindEmpty
	}
	ct := strings.ToLower(d.ContentType())
	if strings.Contains(ct, "json") || sniffJSON(d.Body) {
		return KindJSON
	}
	if strings.Contains(ct, "x-www-form-urlencoded") {
		return KindForm
	}
	return sniffBinaryText(d.Body)
}

func sniffJSON(body []byte) bool {
	t := bytes.TrimLeft(body, " \t\r\n")
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

// sniffBinaryText classifies a non-JSON, non-form body. Control bytes or
// invalid UTF-8 (which would corrupt on string conversion) mean binary.
func sniffBinaryText(body []byte) Kind {
	probe := body
	if len(probe) > 512 {
		probe = probe[:512]
	}
	for _, b := range probe {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			return KindBinary
		}
	}
	if !utf8.Valid(probe) {
		return KindBinary
	}
	return KindText
}
