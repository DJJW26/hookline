// Adversarial probes for delivery.Kind sniffing and accessors. Cases marked
// "wart" pin current behavior that the QA report flags; the tests assert the
// classification does not panic and matches today's implementation.
package delivery

import (
	"regexp"
	"testing"
)

func TestProbeKindEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		body string
		ct   string
		want Kind
	}{
		{name: "whitespace only body is text", body: " \t\r\n ", want: KindText},
		{name: "brace prefixed invalid json sniffs json", body: `{"unterminated`, want: KindJSON},
		{name: "leading whitespace before json sniffs json", body: "\n\t {\"a\":1}  ", want: KindJSON},
		{name: "array prefixed body sniffs json", body: `[1,2`, want: KindJSON},
		{name: "trailing garbage after json opener still json", body: "{ not json at } all", want: KindJSON},
		// Fixed: the declared form type beats JSON sniffing.
		{name: "form ct with brace body stays form", body: `{a=1&b=2}`, ct: "application/x-www-form-urlencoded", want: KindForm},
		{name: "form ct with malformed body stays form", body: "%zz&=&&", ct: "application/x-www-form-urlencoded", want: KindForm},
		{name: "form ct with normal body", body: "a=1&b=2", ct: "application/x-www-form-urlencoded", want: KindForm},
		{name: "invalid utf8 without control bytes is binary", body: "ok\xc3(", want: KindBinary},
		{name: "empty body with json ct is empty", ct: "application/json", want: KindEmpty},
		{name: "crlf laden text stays text", body: "line1\r\nline2\r\n", want: KindText},
		{name: "cjk and emoji json", body: `{"名前":"フック🎉✓"}`, ct: "application/json", want: KindJSON},
		{name: "cjk text without ct", body: "日本語テキスト", want: KindText},
		{name: "emoji text without ct", body: "party 🎉 time", want: KindText},
		{name: "nul control byte is binary", body: "a\x00b", want: KindBinary},
		{name: "escape control byte is binary", body: "a\x1bb", want: KindBinary},
		// Wart: a UTF-8 BOM defeats the sniffer; such bodies fall to text.
		{name: "utf8 bom json without ct falls to text (wart)", body: "\xef\xbb\xbf{\"a\":1}", want: KindText},
		// Fixed: binary bytes beat the declared type (protects cURL export).
		{name: "json ct with gzip magic is binary", body: "\x1f\x8b\x08\x00abc", ct: "application/json", want: KindBinary},
		{name: "tab separated text stays text", body: "a\tb\tc", want: KindText},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Delivery{Body: []byte(tc.body)}
			if tc.ct != "" {
				d.Headers = map[string][]string{"Content-Type": {tc.ct}}
			}
			if got := d.Kind(); got != tc.want {
				t.Errorf("Kind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProbeURLSizeContentType(t *testing.T) {
	d := Delivery{Path: "/p", Query: "q=1"}
	if got := d.URL(); got != "/p?q=1" {
		t.Errorf("URL() = %q, want /p?q=1", got)
	}
	if got := (Delivery{Path: "/p"}).URL(); got != "/p" {
		t.Errorf("URL() no query = %q, want /p", got)
	}
	// Query-only delivery renders a leading "?": no panic, sane output.
	if got := (Delivery{Query: "q=1"}).URL(); got != "?q=1" {
		t.Errorf("URL() query only = %q, want ?q=1", got)
	}
	if got := (Delivery{}).URL(); got != "" {
		t.Errorf("URL() empty = %q, want \"\"", got)
	}
	if got := (Delivery{Body: []byte("abc")}).Size(); got != 3 {
		t.Errorf("Size() = %d, want 3", got)
	}
	// ContentType lookup is case-insensitive and returns the first value.
	ct := Delivery{Headers: map[string][]string{
		"CONTENT-TYPE": {"application/json", "charset=utf-8"},
	}}
	if got := ct.ContentType(); got != "application/json" {
		t.Errorf("ContentType() = %q, want application/json", got)
	}
	if got := (Delivery{}).ContentType(); got != "" {
		t.Errorf("ContentType() none = %q, want \"\"", got)
	}
}

func TestProbeNewID(t *testing.T) {
	idRe := regexp.MustCompile(`^[0-9]{13}-[0-9a-f]{12}$`)
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := NewID()
		if !idRe.MatchString(id) {
			t.Fatalf("NewID() = %q, want <13-digit ms timestamp>-<12 hex chars>", id)
		}
		if seen[id] {
			t.Fatalf("NewID() collision on %q after %d calls", id, i)
		}
		seen[id] = true
	}
}
