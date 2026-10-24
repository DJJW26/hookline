package ui

import (
	"strings"

	"hookline/internal/style"
)

// Line diff for the edit-and-resend review. A compact LCS dynamic program
// is deliberate: webhook payloads are small, and staying dependency-free
// keeps the diff rendering themeable through the same Styles sheet.

// maxDiffLines caps each side before diffing; larger bodies get a notice
// instead of an unbounded matrix.
const maxDiffLines = 2000

// diffLine is one output row of the diff.
type diffLine struct {
	kind byte // ' ', '+', '-'
	text string
}

// lineDiff computes an LCS diff between old and new line slices.
func lineDiff(oldLines, newLines []string) []diffLine {
	n, m := len(oldLines), len(newLines)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []diffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			out = append(out, diffLine{' ', oldLines[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, diffLine{'-', oldLines[i]})
			i++
		default:
			out = append(out, diffLine{'+', newLines[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, diffLine{'-', oldLines[i]})
	}
	for ; j < m; j++ {
		out = append(out, diffLine{'+', newLines[j]})
	}
	return out
}

// renderDiff renders the diff body for the review pane, wrapped to width.
func renderDiff(st style.Styles, oldBody, newBody []byte, width int) string {
	oldLines := splitLines(string(oldBody))
	newLines := splitLines(string(newBody))
	if len(oldLines) > maxDiffLines || len(newLines) > maxDiffLines {
		return st.Note.Render("diff too large to review — send will use the edited body")
	}
	var b strings.Builder
	b.WriteString(st.DiffHunk.Render("--- received") + "\n")
	b.WriteString(st.DiffHunk.Render("+++ edited") + "\n")
	for _, dl := range lineDiff(oldLines, newLines) {
		var ln string
		switch dl.kind {
		case '+':
			ln = st.DiffAdd.Render("+" + dl.text)
		case '-':
			ln = st.DiffDel.Render("-" + dl.text)
		default:
			ln = " " + dl.text
		}
		b.WriteString(ln)
		b.WriteString("\n")
	}
	return b.String()
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
