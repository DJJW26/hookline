package main

import (
	"strings"
	"testing"
)

// --sign validation happens at startup so a typo never silently sends
// unsigned replays; the error must name the bad value and list what is
// supported (the same three providers the flag help advertises).
func TestCheckSignProvider(t *testing.T) {
	if err := checkSignProvider(""); err != nil {
		t.Errorf("empty --sign means no re-signing, got error: %v", err)
	}
	for _, p := range []string{"stripe", "github", "shopify"} {
		if err := checkSignProvider(p); err != nil {
			t.Errorf("--sign %s should be accepted, got: %v", p, err)
		}
	}
	tests := []struct {
		name string
		val  string
	}{
		{"unknown provider", "slack"},
		{"case-sensitive ids", "Stripe"},
		{"empty-ish value", " "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSignProvider(tc.val)
			if err == nil {
				t.Fatalf("--sign %q accepted, want flag error", tc.val)
			}
			if !strings.Contains(err.Error(), tc.val) {
				t.Errorf("error %q does not name the rejected value %q", err, tc.val)
			}
			for _, p := range []string{"stripe", "github", "shopify"} {
				if !strings.Contains(err.Error(), p) {
					t.Errorf("error %q does not list supported provider %q", err, p)
				}
			}
		})
	}
}
