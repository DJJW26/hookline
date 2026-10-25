package style

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestJSONRendersColoredAndWrapped(t *testing.T) {
	forceTrueColor(t)
	s := New(mustLookup(t, "dark"))
	src := []byte(`{"action":"opened","count":42,"ok":true,"meta":null,"nested":{"list":[1,2,3]}}`)
	out := s.JSON(src, 40)

	if !strings.Contains(out, "\x1b[") {
		t.Error("expected ANSI colour codes in highlighted JSON")
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line %d width %d exceeds 40: %q", i, w, line)
		}
	}
	// Content survives highlighting and wrapping.
	plain := stripANSI(out)
	for _, want := range []string{`"action"`, `"opened"`, "42", "true", "null", "1,", "3"} {
		if !strings.Contains(plain, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestJSONInvalidFallsBackToPlain(t *testing.T) {
	s := New(mustLookup(t, "dark"))
	src := []byte("this is { not json")
	out := s.JSON(src, 40)
	if !strings.Contains(stripANSI(out), "this is { not json") {
		t.Errorf("plain fallback lost content: %q", out)
	}
}

func TestJSONKeyVsStringValue(t *testing.T) {
	forceTrueColor(t)
	s := New(mustLookup(t, "dark"))
	out := s.JSON([]byte(`{"key":"value"}`), 40)
	esc := "\x1b["
	if strings.Count(out, esc) < 2 {
		t.Error("expected at least key and value to be coloured separately")
	}
}

func TestPlainEscapesControlBytes(t *testing.T) {
	s := New(mustLookup(t, "mono"))
	out := s.Plain([]byte("a\x00b\x1fc\nd"), 40)
	if !strings.Contains(out, `\x00`) || !strings.Contains(out, `\x1f`) {
		t.Errorf("control bytes not escaped: %q", out)
	}
	if !strings.Contains(out, "a") || !strings.Contains(out, "c") {
		t.Error("readable bytes lost")
	}
}

func TestHexDumpLayout(t *testing.T) {
	s := New(mustLookup(t, "dark"))
	body := []byte("hookline hexdump test 1234567890 ABCDEFGHIJKLMNOP")
	out := s.HexDump(body, 80)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected several hexdump lines, got %d", len(lines))
	}
	if !strings.HasPrefix(stripANSI(lines[0]), "00000000") {
		t.Errorf("first line lacks offset: %q", lines[0])
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "hookline") {
		t.Error("ascii column lost content")
	}
	// Offsets increase by 0x10 per line.
	if !strings.Contains(stripANSI(lines[1]), "00000010") {
		t.Errorf("second line offset wrong: %q", lines[1])
	}
}

func TestFormKeepsPairs(t *testing.T) {
	forceTrueColor(t)
	s := New(mustLookup(t, "dark"))
	out := s.Form([][2]string{{"a", "1"}, {"b", "2 3"}, {"a", "again"}}, 40)
	plain := stripANSI(out)
	for _, want := range []string{"a", "1", "b", "2 3", "again"} {
		if !strings.Contains(plain, want) {
			t.Errorf("form render missing %q", want)
		}
	}
}

func TestTruncateEndANSIAware(t *testing.T) {
	forceTrueColor(t)
	s := New(mustLookup(t, "dark"))
	long := strings.Repeat("x", 60)
	colored := s.Method("GET").Render(long)
	got := s.TruncateEnd(colored, 20)
	if w := lipgloss.Width(got); w > 20 {
		t.Errorf("truncated width %d > 20", w)
	}
	if !strings.HasSuffix(stripANSI(got), GlyphEllipsis) {
		t.Errorf("expected trailing ellipsis, got %q", got)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestWrapZeroWidthNoop(t *testing.T) {
	in := "line one\nline two"
	if got := wrap(in, 0); got != in {
		t.Errorf("wrap(_, 0) should be a no-op, got %q", got)
	}
}

func TestFormatHexPadsShortChunks(t *testing.T) {
	got := formatHex([]byte{0x41})
	if !strings.HasSuffix(got, strings.Repeat("   ", 15)) {
		t.Errorf("short chunk not padded for alignment: %q", got)
	}
	if !strings.HasPrefix(got, "41") {
		t.Errorf("hex wrong: %q", got)
	}
}

func TestJSONLongStringKeyDetection(t *testing.T) {
	// A string containing escaped quotes and a colon must not confuse the
	// key/string classifier.
	forceTrueColor(t)
	s := New(mustLookup(t, "dark"))
	src := []byte(`{"weird key: \"quoted\"": "value: with colon"}`)
	out := s.JSON(src, 200)
	plain := stripANSI(out)
	if !strings.Contains(plain, `weird key: \"quoted\"`) {
		t.Errorf("escaped key mangled: %q", plain)
	}
	if !strings.Contains(plain, "value: with colon") {
		t.Errorf("value mangled: %q", plain)
	}
}

func TestWrapBreaksUnbreakableRuns(t *testing.T) {
	// A 2000-character token with no spaces must hard-wrap, never blow the
	// frame width (regression: wordwrap alone passes long runs through).
	s := New(mustLookup(t, "mono"))
	out := s.Plain([]byte(strings.Repeat("x", 2000)), 60)
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Fatalf("line %d width %d exceeds 60", i, w)
		}
	}
}
