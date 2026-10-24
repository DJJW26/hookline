package style

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Glyphs are the handful of symbols the UI uses, centralised so the whole
// app stays visually consistent. They are plain UTF-8 (not nerd-font icons)
// so every terminal can render them.
const (
	GlyphBrand    = "◆" // wordmark
	GlyphLive     = "⏺" // listener status
	GlyphUnread   = "•" // new delivery marker
	GlyphBullet   = "·" // soft separator
	GlyphEllipsis = "…"
)

// Styles is the complete, immutable style sheet for one Theme. Construct it
// once at startup and pass it down; the UI treats it as read-only.
type Styles struct {
	Theme Theme

	// Header line.
	Brand    lipgloss.Style // "◆ hookline" wordmark
	HeaderID lipgloss.Style // listening info, right side
	Live     lipgloss.Style // live dot + label
	Idle     lipgloss.Style // not-listening state

	// Panels.
	Panel      lipgloss.Style // rounded, muted border
	PanelFocus lipgloss.Style // rounded, accent border (focused pane)
	PanelTitle lipgloss.Style // title text drawn over the border
	PanelNote  lipgloss.Style // right-aligned title note (counts, filters)

	// List rows. RowSelected/RowSelectedLive are the subtle selection
	// backgrounds for an unfocused/focused list pane.
	RowSelected     lipgloss.Style
	RowSelectedLive lipgloss.Style
	RowTime         lipgloss.Style
	RowDim          lipgloss.Style // size / kind metadata
	Unread          lipgloss.Style
	Empty           lipgloss.Style // empty-state copy
	EmptyCode       lipgloss.Style // sample command in empty state

	// Detail pane.
	Tab       lipgloss.Style
	TabActive lipgloss.Style
	Body      lipgloss.Style // rendered body text
	Note      lipgloss.Style // notices (truncation, hints)

	// Status bar.
	Status    lipgloss.Style // resting status line
	StatusKey lipgloss.Style // keycap hints
	StatusDim lipgloss.Style // de-emphasised info
	ToastGood lipgloss.Style
	ToastWarn lipgloss.Style
	ToastBad  lipgloss.Style

	// Input line (search / target prompt).
	InputLabel  lipgloss.Style
	InputText   lipgloss.Style
	InputCursor lipgloss.Style

	// Help overlay.
	HelpPanel  lipgloss.Style
	HelpTitle  lipgloss.Style
	HelpKey    lipgloss.Style
	HelpAction lipgloss.Style

	// Diff.
	DiffAdd  lipgloss.Style
	DiffDel  lipgloss.Style
	DiffHunk lipgloss.Style
}

// New builds the style sheet for a theme. Panels are rounded — the one
// signature shape of the app — with generous inner padding on the detail
// pane and none on rows, which carry their own spacing.
func New(t Theme) Styles {
	base := lipgloss.NewStyle().Foreground(t.Base)

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Border)

	s := Styles{
		Theme: t,

		Brand:    base.Bold(true).Foreground(t.Accent),
		HeaderID: lipgloss.NewStyle().Foreground(t.Dim),
		Live:     lipgloss.NewStyle().Foreground(t.Accent2),
		Idle:     lipgloss.NewStyle().Foreground(t.Faint),

		Panel:      panel,
		PanelFocus: panel.BorderForeground(t.BorderFocus),
		PanelTitle: base.Bold(true).Foreground(t.Title),
		PanelNote:  lipgloss.NewStyle().Foreground(t.Faint),

		RowSelected:     base.Background(t.Surface),
		RowSelectedLive: base.Background(t.SurfaceLive),
		RowTime:         lipgloss.NewStyle().Foreground(t.Faint),
		RowDim:          lipgloss.NewStyle().Foreground(t.Dim),
		Unread:          lipgloss.NewStyle().Foreground(t.Accent),
		Empty:           lipgloss.NewStyle().Foreground(t.Dim),
		EmptyCode:       lipgloss.NewStyle().Foreground(t.Accent),

		Tab:       lipgloss.NewStyle().Foreground(t.Dim),
		TabActive: base.Bold(true).Foreground(t.Accent),
		Body:      base,
		Note:      lipgloss.NewStyle().Foreground(t.Warn),

		Status:    lipgloss.NewStyle().Foreground(t.Dim),
		StatusKey: lipgloss.NewStyle().Foreground(t.Base),
		StatusDim: lipgloss.NewStyle().Foreground(t.Faint),
		ToastGood: lipgloss.NewStyle().Foreground(t.Good),
		ToastWarn: lipgloss.NewStyle().Foreground(t.Warn),
		ToastBad:  lipgloss.NewStyle().Foreground(t.Bad),

		InputLabel:  base.Bold(true).Foreground(t.Accent),
		InputText:   base,
		InputCursor: base.Reverse(true),

		HelpPanel:  panel.Padding(1, 3),
		HelpTitle:  base.Bold(true).Foreground(t.Accent),
		HelpKey:    lipgloss.NewStyle().Foreground(t.Base),
		HelpAction: lipgloss.NewStyle().Foreground(t.Dim),

		DiffAdd:  base.Foreground(t.Good),
		DiffDel:  base.Foreground(t.Bad),
		DiffHunk: lipgloss.NewStyle().Foreground(t.Dim),
	}
	return s
}

// Method returns the style for an HTTP method verb, lower-cased for display.
// Unknown verbs fall back to the Other colour so nothing ever renders unstyled.
func (s Styles) Method(method string) lipgloss.Style {
	var c lipgloss.Color
	switch strings.ToUpper(method) {
	case "GET":
		c = s.Theme.Methods.Get
	case "POST":
		c = s.Theme.Methods.Post
	case "PUT":
		c = s.Theme.Methods.Put
	case "PATCH":
		c = s.Theme.Methods.Patch
	case "DELETE":
		c = s.Theme.Methods.Delete
	default:
		c = s.Theme.Methods.Other
	}
	return lipgloss.NewStyle().Bold(true).Foreground(c)
}

// Key renders a label with the same colour JSON object keys use, so header
// names and JSON keys share one visual identity across panes.
func (s Styles) Key(text string) string {
	return lipgloss.NewStyle().Foreground(s.Theme.JSON.Key).Render(text)
}

// StatusStyle maps an HTTP status code to a feedback style.
func (s Styles) StatusStyle(code int) lipgloss.Style {
	switch {
	case code >= 200 && code < 300:
		return lipgloss.NewStyle().Foreground(s.Theme.Good)
	case code >= 300 && code < 400:
		return lipgloss.NewStyle().Foreground(s.Theme.Dim)
	default:
		return lipgloss.NewStyle().Foreground(s.Theme.Bad)
	}
}
