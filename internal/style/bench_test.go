package style

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// benchDoc is a fixed, deterministic ~8 KiB indented JSON document — the
// shape a captured webhook body has when the detail pane pretty-prints it.
var benchDoc = buildBenchDoc()

func buildBenchDoc() string {
	var b strings.Builder
	b.WriteString("{\n")
	for i := 0; b.Len() < 8<<10-64; i++ {
		fmt.Fprintf(&b, "  \"event.field.%04d\": \"%s\",\n", i, strings.Repeat("payload ", 8))
	}
	b.WriteString("  \"done\": true,\n  \"count\": 12345\n}\n")
	return b.String()
}

// BenchmarkJSONTokenize colorizes the fixed ~8 KiB document at width 80 —
// the tokenise + colour + wrap pipeline behind the body tab. The profile is
// pinned the same way the package's tests pin it, so allocations never
// depend on the runner's terminal.
func BenchmarkJSONTokenize(b *testing.B) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	st := New(mustBenchTheme(b))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := st.JSONPretty(benchDoc, 80); out == "" {
			b.Fatal("empty render")
		}
	}
}

func mustBenchTheme(b *testing.B) Theme {
	b.Helper()
	th, err := Lookup("dark")
	if err != nil {
		b.Fatalf("lookup dark theme: %v", err)
	}
	return th
}
