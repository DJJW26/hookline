package style

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// forceTrueColor sets a truecolour profile on lipgloss's default renderer so
// tests can assert on real ANSI output regardless of the test runner's own
// terminal (or lack of one).
func forceTrueColor(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
}

func TestLookup(t *testing.T) {
	for _, id := range []string{"dark", "light", "mono"} {
		th, err := Lookup(id)
		if err != nil {
			t.Fatalf("Lookup(%q) error: %v", id, err)
		}
		if th.ID != id {
			t.Errorf("Lookup(%q).ID = %q", id, th.ID)
		}
	}
	if _, err := Lookup("does-not-exist"); err == nil {
		t.Error("Lookup(unknown) should error")
	}
}

func TestIDsIncludeBuiltins(t *testing.T) {
	ids := IDs()
	for _, want := range []string{"dark", "light", "mono"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Errorf("IDs() missing %q (got %v)", want, ids)
		}
	}
}

func TestDetect(t *testing.T) {
	if _, err := Detect("auto"); err != nil {
		t.Errorf("Detect(auto) error: %v", err)
	}
	th, err := Detect("mono")
	if err != nil {
		t.Fatalf("Detect(mono) error: %v", err)
	}
	if th.ID != "mono" {
		t.Errorf("Detect(mono).ID = %q", th.ID)
	}
}

func TestNewStylesBuildsAllThemes(t *testing.T) {
	for _, id := range IDs() {
		th, err := Lookup(id)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", id, err)
		}
		s := New(th)
		if s.Theme.ID != th.ID {
			t.Errorf("New(%q) did not carry the theme", th.ID)
		}
		if got := stripANSI(s.Method("POST").Render("POST")); got != "POST" {
			t.Errorf("Method(POST) should render the verb, got %q", got)
		}
	}
}

func TestMethodKnownAndUnknown(t *testing.T) {
	forceTrueColor(t)
	s := New(mustLookup(t, "dark"))
	if got := s.Method("GET"); got.String() == s.Method("FANCY").String() {
		t.Error("GET should be styled differently from an unknown verb")
	}
	// Unknown verbs must still resolve to a concrete style, not zero value.
	if s.Method("PROPFIND").GetBold() != true {
		t.Error("Method() should stay bold for unknown verbs")
	}
}

func mustLookup(t *testing.T, id string) Theme {
	t.Helper()
	th, err := Lookup(id)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", id, err)
	}
	return th
}
