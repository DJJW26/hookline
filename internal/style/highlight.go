package style

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/reflow/wordwrap"
	hardwrap "github.com/muesli/reflow/wrap"
)

// jsonKind classifies JSON tokens for colouring.
type jsonKind int

const (
	jsonPlain jsonKind = iota
	jsonKey
	jsonStr
	jsonNum
	jsonBool
	jsonNull
	jsonPunct
	jsonBrace
)

type jsonToken struct {
	kind jsonKind
	text string
}

// JSON renders a JSON body pretty-printed and syntax-coloured, wrapped to
// width. Bodies that fail to parse are returned as escaped plain text.
func (s Styles) JSON(src []byte, width int) string {
	var indented bytes.Buffer
	if err := json.Indent(&indented, src, "", "  "); err != nil {
		return s.Plain(src, width)
	}
	return s.JSONPretty(indented.String(), width)
}

// JSONPretty colourises and wraps already-indented JSON. It never parses,
// so it is safe to hand it a truncated slice of a larger document.
func (s Styles) JSONPretty(pretty string, width int) string {
	var b strings.Builder
	for _, tk := range tokenizeJSON(pretty) {
		if tk.kind == jsonPlain {
			b.WriteString(tk.text)
			continue
		}
		b.WriteString(s.jsonStyle(tk.kind).Render(tk.text))
	}
	return wrap(b.String(), width)
}

// Plain renders arbitrary text, made visible-safe, wrapped to width.
func (s Styles) Plain(src []byte, width int) string {
	return wrap(s.Body.Render(escapeText(src)), width)
}

// Form renders a urlencoded body as aligned key/value lines.
func (s Styles) Form(pairs [][2]string, width int) string {
	max := 0
	for _, p := range pairs {
		if n := len(p[0]); n > max {
			max = n
		}
	}
	if max > 24 {
		max = 24
	}
	var b strings.Builder
	for _, p := range pairs {
		key := s.jsonStyle(jsonKey).Render(fmt.Sprintf("%-*s", max, p[0]))
		val := s.jsonStyle(jsonStr).Render(p[1])
		b.WriteString(key + "  " + val + "\n")
	}
	return wrap(b.String(), width)
}

// HexDump renders the first bytes of a binary body in a classic
// offset/hex/ascii layout, three columns visually separated by colour.
func (s Styles) HexDump(src []byte, width int) string {
	limit := len(src)
	if limit > 4096 {
		limit = 4096
	}
	var b strings.Builder
	for off := 0; off < limit; off += 16 {
		end := min(off+16, limit)
		chunk := src[off:end]
		hexPart := formatHex(chunk)
		ascii := formatASCII(chunk)
		b.WriteString(s.RowTime.Render(fmt.Sprintf("%08x", off)))
		b.WriteString("  ")
		b.WriteString(s.Body.Render(hexPart))
		b.WriteString("  ")
		b.WriteString(s.RowDim.Render(ascii))
		b.WriteString("\n")
	}
	if limit < len(src) {
		b.WriteString(s.Note.Render(fmt.Sprintf("… %d more bytes", len(src)-limit)))
	}
	return wrap(b.String(), width)
}

func (s Styles) jsonStyle(k jsonKind) lipgloss.Style {
	j := s.Theme.JSON
	switch k {
	case jsonKey:
		return lipgloss.NewStyle().Foreground(j.Key)
	case jsonStr:
		return lipgloss.NewStyle().Foreground(j.Str)
	case jsonNum:
		return lipgloss.NewStyle().Foreground(j.Num)
	case jsonBool:
		return lipgloss.NewStyle().Foreground(j.Bool)
	case jsonNull:
		return lipgloss.NewStyle().Foreground(j.Null)
	case jsonPunct:
		return lipgloss.NewStyle().Foreground(j.Punct)
	case jsonBrace:
		return lipgloss.NewStyle().Foreground(j.Brace)
	default:
		return lipgloss.NewStyle()
	}
}

// tokenizeJSON splits pretty-printed JSON into colourable tokens without
// building an AST: a small state machine that understands strings (and the
// colon lookahead that distinguishes keys), numbers, literals and structure.
func tokenizeJSON(src string) []jsonToken {
	var toks []jsonToken
	runes := []rune(src)
	n := len(runes)
	for i := 0; i < n; {
		c := runes[i]
		switch {
		case c == '"':
			j := i + 1
			for j < n {
				if runes[j] == '\\' {
					j += 2
					continue
				}
				if runes[j] == '"' {
					j++
					break
				}
				j++
			}
			text := string(runes[i:min(j, n)])
			k := j
			for k < n && (runes[k] == ' ' || runes[k] == '\t') {
				k++
			}
			if k < n && runes[k] == ':' {
				toks = append(toks, jsonToken{jsonKey, text})
			} else {
				toks = append(toks, jsonToken{jsonStr, text})
			}
			i = j
		case c == '-' || (c >= '0' && c <= '9'):
			j := i
			for j < n && strings.ContainsRune("-+.eE0123456789", runes[j]) {
				j++
			}
			toks = append(toks, jsonToken{jsonNum, string(runes[i:j])})
			i = j
		case (c >= 'a' && c <= 'z'):
			j := i
			for j < n && runes[j] >= 'a' && runes[j] <= 'z' {
				j++
			}
			text := string(runes[i:j])
			switch text {
			case "true", "false":
				toks = append(toks, jsonToken{jsonBool, text})
			case "null":
				toks = append(toks, jsonToken{jsonNull, text})
			default:
				toks = append(toks, jsonToken{jsonPlain, text})
			}
			i = j
		case c == '{' || c == '}' || c == '[' || c == ']':
			toks = append(toks, jsonToken{jsonBrace, string(c)})
			i++
		case c == ':' || c == ',':
			toks = append(toks, jsonToken{jsonPunct, string(c)})
			i++
		default:
			j := i
			for j < n {
				r := runes[j]
				if r == '"' || r == '{' || r == '}' || r == '[' || r == ']' ||
					r == ':' || r == ',' || r == '-' ||
					(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') {
					break
				}
				j++
			}
			if j == i {
				j++
			}
			toks = append(toks, jsonToken{jsonPlain, string(runes[i:j])})
			i = j
		}
	}
	return toks
}

// wrap breaks ANSI-coloured text at width: word wrap first for nice
// breaks, then a hard wrap so unbreakable runs (a 2000-char token, a long
// hex line) can never exceed the width. Width <= 0 returns the text
// unchanged.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	return hardwrap.String(wordwrap.String(s, width), width)
}

// Wrap re-exposes the ANSI-safe wrap for callers outside the package
// (the diff view needs the same guarantees).
func (s Styles) Wrap(text string, width int) string {
	return wrap(text, width)
}

// TruncateEnd shortens a single line (possibly ANSI-coloured) to width,
// appending an ellipsis when cut.
func (s Styles) TruncateEnd(line string, width int) string {
	if width <= 0 {
		return line
	}
	return truncate.StringWithTail(line, uint(width), GlyphEllipsis)
}

// escapeText makes arbitrary bytes printable: control characters other than
// newline and tab become their \xNN form.
func escapeText(src []byte) string {
	var b strings.Builder
	for _, c := range src {
		switch {
		case c == '\n' || c == '\t':
			b.WriteByte(c)
		case c < 0x20 || c == 0x7F:
			fmt.Fprintf(&b, "\\x%02x", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func formatHex(chunk []byte) string {
	var b strings.Builder
	for i, c := range chunk {
		fmt.Fprintf(&b, "%02x", c)
		if i == 7 {
			b.WriteByte(' ')
		}
		if i < len(chunk)-1 {
			b.WriteByte(' ')
		}
	}
	// Pad short chunks so the ascii column stays aligned.
	if pad := 16 - len(chunk); pad > 0 {
		b.WriteString(strings.Repeat("   ", pad))
	}
	return b.String()
}

func formatASCII(chunk []byte) string {
	var b strings.Builder
	for _, c := range chunk {
		if c >= 0x20 && c < 0x7F {
			b.WriteByte(c)
		} else {
			b.WriteByte('.')
		}
	}
	return b.String()
}
