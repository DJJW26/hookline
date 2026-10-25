package replay

import (
	"strings"
	"testing"

	"github.com/DJJW26/hookline/internal/delivery"
)

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "plain", want: "'plain'"},
		{in: "", want: "''"},
		{in: "it's", want: `'it'\''s'`},
		{in: "a'b'c", want: `'a'\''b'\''c'`},
		{in: "double \"quote\"", want: `'double "quote"'`},
		{in: "$HOME `cmd` \\ ;|&", want: `'$HOME ` + "`cmd`" + ` \ ;|&'`},
	}
	for _, tc := range tests {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestToCurlHeaderFlagsCount(t *testing.T) {
	d := delivery.Delivery{
		Method: "POST",
		Path:   "/c",
		Headers: map[string][]string{
			"B": {"2", "1"}, // values render in stored order
			"A": {"0"},
		},
		Body: []byte("x"),
	}
	out := ToCurl("http://h", d)
	// Header names render sorted regardless of map iteration order.
	aIdx := strings.Index(out, "-H 'A: 0'")
	b1Idx := strings.Index(out, "-H 'B: 2'")
	b2Idx := strings.Index(out, "-H 'B: 1'")
	if aIdx < 0 || b1Idx < 0 || b2Idx < 0 {
		t.Fatalf("missing -H flags in output: %s", out)
	}
	if aIdx > b1Idx || aIdx > b2Idx {
		t.Errorf("header A must render before header B: %s", out)
	}
	if got := strings.Count(out, "-H "); got != 3 {
		t.Errorf("count of -H flags = %d, want 3 (output: %s)", got, out)
	}
}

func TestToCurlInvalidUTF8TreatedAsBinary(t *testing.T) {
	// High-bit invalid UTF-8 must not be stringified (U+FFFD corruption);
	// it belongs on the @file path.
	d := delivery.Delivery{
		Method:  "POST",
		Path:    "/bin",
		Headers: map[string][]string{},
		Body:    []byte{'o', 'k', 0xC3, 0x28}, // invalid sequence
	}
	got := ToCurl("http://localhost:4040", d)
	if !strings.Contains(got, "binary body") {
		t.Errorf("invalid UTF-8 should take the binary path, got %q", got)
	}
}
