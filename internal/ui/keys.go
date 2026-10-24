package ui

// keyHint pairs a keycap with its action. The help overlay renders this
// table; the status line shows a compact, mode-aware subset defined next
// to it in view.go — keep the two consistent when editing either.
type keyHint struct {
	key    string
	action string
}

// Help sections, in display order. The status line shows the first few
// hints of the relevant section; "?" shows everything.
var helpSections = []struct {
	title string
	hints []keyHint
}{
	{
		title: "navigate",
		hints: []keyHint{
			{"↑/↓ j/k", "move selection / scroll"},
			{"tab", "switch pane"},
			{"enter", "inspect delivery"},
			{"esc", "back / cancel"},
			{"g G", "jump to top / bottom"},
		},
	},
	{
		title: "actions",
		hints: []keyHint{
			{"r", "replay to target"},
			{"R", "replay latest"},
			{"e", "edit body & resend"},
			{"c", "copy as cURL"},
			{"y", "copy raw body"},
			{"t", "set replay target"},
			{"/", "find deliveries"},
			{"f", "filter by method"},
			{"x", "clear all deliveries"},
		},
	},
	{
		title: "general",
		hints: []keyHint{
			{"?", "toggle help"},
			{"q", "quit"},
			{"ctrl+c", "force quit"},
		},
	},
	{
		title: "detail tabs",
		hints: []keyHint{
			{"1-4 h/l", "body / headers / raw / response"},
		},
	},
}
