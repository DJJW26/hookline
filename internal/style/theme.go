// Package style owns every colour, border and typographic decision in the
// UI. The UI layer never builds a lipgloss.Style of its own; it receives a
// Styles value built from a Theme. Adding a theme means registering a new
// Theme value — nothing else changes.
//
// Design language: "Dusk" (the default) takes its cues from Charm's Crush
// (the #6B50FF purple accent family and calm text ladder, rounded panels,
// one accent used sparingly) and from Claude Code's minimalism (no chrome
// that does not inform, muted status line, restrained feedback tones).
// Backgrounds are deliberately left transparent so the terminal's own
// theme — and the user's terminal customisations — show through. The app
// should feel like it was always part of the user's terminal, not stamped
// on top of it.
package style

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// A Theme is a named palette of semantic colours. Every field is a role, not
// a location: "accent" means the same thing in the header, the list and the
// diff view. Themes that want to blend into any terminal leave backgrounds
// transparent by using lipgloss.Color("").
type Theme struct {
	ID   string // stable identifier used by --theme
	Name string // human-readable name shown in the UI

	// Base text roles.
	Base  lipgloss.Color // primary foreground
	Dim   lipgloss.Color // secondary text: metadata, labels
	Faint lipgloss.Color // tertiary text: hints, placeholders

	// Accents. Accent is the brand colour (wordmark, focus, selection).
	// Accent2 is reserved for the live indicator and nothing else.
	Accent  lipgloss.Color
	Accent2 lipgloss.Color

	// Feedback.
	Good lipgloss.Color
	Warn lipgloss.Color
	Bad  lipgloss.Color

	// Chrome.
	Border      lipgloss.Color // unfocused panel borders
	BorderFocus lipgloss.Color // focused panel borders
	Title       lipgloss.Color // panel titles
	Surface     lipgloss.Color // selected-row background (subtle)
	SurfaceLive lipgloss.Color // selected-row background when list focused

	// Per-HTTP-method colours, uniform across list, detail and exports.
	Methods MethodSet

	// JSON syntax colours.
	JSON JSONSet
}

// MethodSet colours HTTP methods.
type MethodSet struct {
	Get    lipgloss.Color
	Post   lipgloss.Color
	Put    lipgloss.Color
	Patch  lipgloss.Color
	Delete lipgloss.Color
	Other  lipgloss.Color
}

// JSONSet colours JSON tokens when rendering bodies.
type JSONSet struct {
	Key   lipgloss.Color
	Str   lipgloss.Color
	Num   lipgloss.Color
	Bool  lipgloss.Color
	Null  lipgloss.Color
	Punct lipgloss.Color
	Brace lipgloss.Color
}

// registry holds every known theme. Built-ins are registered below; callers
// may register their own via Register (kept for future config-file themes).
var registry = map[string]Theme{}

// Register adds a theme to the registry, replacing any existing theme with
// the same ID. It is safe to call during package initialisation.
func Register(t Theme) {
	registry[t.ID] = t
}

// Lookup returns the theme with the given ID.
func Lookup(id string) (Theme, error) {
	t, ok := registry[id]
	if !ok {
		return Theme{}, fmt.Errorf("style: unknown theme %q", id)
	}
	return t, nil
}

// IDs returns all registered theme IDs, sorted.
func IDs() []string {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Detect resolves the --theme flag value. "auto" (the default) picks dark
// or light based on the terminal's reported background, falling back to
// dark when the terminal cannot be queried (pipes, minimal terminals).
func Detect(prefers string) (Theme, error) {
	if prefers == "" || prefers == "auto" {
		if termenv.HasDarkBackground() {
			return Lookup("dark")
		}
		return Lookup("light")
	}
	return Lookup(prefers)
}

// dusk is the default dark palette. Colour roles take their cues from
// Charm's Crush (Charmtone Pantera): the #6B50FF purple family for accent
// and focus, Crush's calm text ladder (Sash/Iron) for Base/Dim/Faint, and
// its muted diff pair for feedback tones — but softened so text stays
// comfortable on any dark background, since we never paint our own.
func dusk() Theme {
	return Theme{
		ID:          "dark",
		Name:        "Dusk",
		Base:        lipgloss.Color("#ECEBF0"),
		Dim:         lipgloss.Color("#858392"),
		Faint:       lipgloss.Color("#605F6B"),
		Accent:      lipgloss.Color("#9D8CFF"),
		Accent2:     lipgloss.Color("#FF985A"),
		Good:        lipgloss.Color("#12C78F"),
		Warn:        lipgloss.Color("#D4C55C"),
		Bad:         lipgloss.Color("#EB4268"),
		Border:      lipgloss.Color("#3A3943"),
		BorderFocus: lipgloss.Color("#6B50FF"),
		Title:       lipgloss.Color("#858392"),
		Surface:     lipgloss.Color("#2D2C36"),
		SurfaceLive: lipgloss.Color("#343148"),
		Methods: MethodSet{
			Get:    lipgloss.Color("#7FA8E8"),
			Post:   lipgloss.Color("#7FC98F"),
			Put:    lipgloss.Color("#D4C55C"),
			Patch:  lipgloss.Color("#C77DFF"),
			Delete: lipgloss.Color("#EB4268"),
			Other:  lipgloss.Color("#858392"),
		},
		JSON: JSONSet{
			Key:   lipgloss.Color("#9D8CFF"),
			Str:   lipgloss.Color("#8FBF8F"),
			Num:   lipgloss.Color("#D4C55C"),
			Bool:  lipgloss.Color("#FF985A"),
			Null:  lipgloss.Color("#C77DFF"),
			Punct: lipgloss.Color("#858392"),
			Brace: lipgloss.Color("#858392"),
		},
	}
}

// linen is the light counterpart: ink-on-paper tones for terminals running
// light backgrounds. Accents are darkened to keep contrast. (Crush ships
// only dark themes; light support here is part of blending into whatever
// terminal the user actually runs.)
func linen() Theme {
	return Theme{
		ID:          "light",
		Name:        "Linen",
		Base:        lipgloss.Color("#35323A"),
		Dim:         lipgloss.Color("#6E6875"),
		Faint:       lipgloss.Color("#A29CAB"),
		Accent:      lipgloss.Color("#5B44D6"),
		Accent2:     lipgloss.Color("#C05F2E"),
		Good:        lipgloss.Color("#0E8F6B"),
		Warn:        lipgloss.Color("#8F8534"),
		Bad:         lipgloss.Color("#C43054"),
		Border:      lipgloss.Color("#D8D2DC"),
		BorderFocus: lipgloss.Color("#6B50FF"),
		Title:       lipgloss.Color("#6E6875"),
		Surface:     lipgloss.Color("#ECEBF0"),
		SurfaceLive: lipgloss.Color("#E4E0F2"),
		Methods: MethodSet{
			Get:    lipgloss.Color("#3E68B0"),
			Post:   lipgloss.Color("#2E7D4F"),
			Put:    lipgloss.Color("#8F7A2E"),
			Patch:  lipgloss.Color("#6B44B0"),
			Delete: lipgloss.Color("#C43054"),
			Other:  lipgloss.Color("#6E6875"),
		},
		JSON: JSONSet{
			Key:   lipgloss.Color("#5B44D6"),
			Str:   lipgloss.Color("#2E7D4F"),
			Num:   lipgloss.Color("#8F7A2E"),
			Bool:  lipgloss.Color("#C05F2E"),
			Null:  lipgloss.Color("#6B44B0"),
			Punct: lipgloss.Color("#6E6875"),
			Brace: lipgloss.Color("#6E6875"),
		},
	}
}

// graphite is the zero-chrome theme: no colour output at all, hierarchy via
// weight and brackets only. It is the safety net for minimal terminals and
// for users who run NO_COLOR.
func graphite() Theme {
	empty := lipgloss.Color("")
	t := Theme{
		ID:   "mono",
		Name: "Graphite",
	}
	t.Base, t.Dim, t.Faint = empty, empty, empty
	t.Accent, t.Accent2 = empty, empty
	t.Good, t.Warn, t.Bad = empty, empty, empty
	t.Border, t.BorderFocus, t.Title = empty, empty, empty
	t.Surface, t.SurfaceLive = empty, empty
	t.Methods = MethodSet{Other: empty}
	t.Methods.Get, t.Methods.Post, t.Methods.Put = empty, empty, empty
	t.Methods.Patch, t.Methods.Delete = empty, empty
	t.JSON = JSONSet{Key: empty, Str: empty, Num: empty, Bool: empty, Null: empty, Punct: empty, Brace: empty}
	return t
}

func init() {
	Register(dusk())
	Register(linen())
	Register(graphite())
}
