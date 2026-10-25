package replay

import (
	"sort"
	"strconv"
	"strings"

	"github.com/DJJW26/hookline/internal/delivery"
)

// ToCurl renders d as a one-line cURL command targeting baseURL+path+query,
// ready to copy and paste into a shell. Header values, the URL and the body
// are single-quoted with embedded quotes escaped POSIX-style. Binary bodies
// are not inlined; the command carries a trailing comment instructing the
// user to save the body and pass it via --data-binary @file.
func ToCurl(baseURL string, d delivery.Delivery) string {
	var sb strings.Builder
	sb.WriteString("curl -X ")
	sb.WriteString(d.Method)
	sb.WriteString(" ")
	sb.WriteString(shellQuote(strings.TrimSuffix(baseURL, "/") + d.URL()))
	for _, name := range sortedHeaderNames(d.Headers) {
		for _, v := range d.Headers[name] {
			sb.WriteString(" -H ")
			sb.WriteString(shellQuote(name + ": " + v))
		}
	}
	if d.Kind() == delivery.KindBinary {
		sb.WriteString("  # binary body (")
		sb.WriteString(strconv.Itoa(len(d.Body)))
		sb.WriteString(" bytes): save and pass --data-binary @file")
		return sb.String()
	}
	if len(d.Body) > 0 {
		sb.WriteString(" --data-binary ")
		sb.WriteString(shellQuote(string(d.Body)))
	}
	return sb.String()
}

// sortedHeaderNames returns the header names in d.Headers in sorted order so
// rendered commands are deterministic.
func sortedHeaderNames(h map[string][]string) []string {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// shellQuote wraps s in single quotes so a shell treats it literally. An
// embedded single quote is escaped by closing the quoted span, emitting a
// backslash-escaped quote, and reopening the span.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
