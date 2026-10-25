package ui

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"hookline/internal/delivery"
)

// Formatting helpers. Small, pure, uniform: one way to render a size, one
// way to render a time, everywhere in the UI.

// humanSize renders a byte count as engineers expect: 812B, 1.2K, 3.4M.
func humanSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
}

// dayTime renders a delivery timestamp relative to now: clock time for
// today, weekday for this week, date for older — yesterday's history must
// not masquerade as today's.
func dayTime(t time.Time) string {
	now := time.Now()
	local := t.Local()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04:05")
	}
	if now.Sub(local) < 7*24*time.Hour {
		return local.Format("Mon 15:04")
	}
	return local.Format("Jan 02 15:04")
}

// methodLabel pads a verb to the width of the longest common method so the
// path column stays aligned regardless of verb.
func methodLabel(m string) string {
	if len(m) >= 6 {
		return m
	}
	return m + strings.Repeat(" ", 6-len(m))
}

// formPairs parses a urlencoded body preserving key order — repeated keys
// stay adjacent, which url.Values (a map) would scramble.
func formPairs(body []byte) [][2]string {
	var pairs [][2]string
	for _, part := range strings.Split(string(body), "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		ku, err := url.QueryUnescape(k)
		if err != nil {
			ku = k
		}
		vu, err := url.QueryUnescape(v)
		if err != nil {
			vu = v
		}
		pairs = append(pairs, [2]string{ku, vu})
	}
	return pairs
}

// kindLabel is the one-character body-kind tag shown in list rows.
func kindLabel(d delivery.Delivery) string {
	switch d.Kind() {
	case delivery.KindJSON:
		return "json"
	case delivery.KindForm:
		return "form"
	case delivery.KindBinary:
		return "bin"
	case delivery.KindText:
		return "text"
	default:
		return ""
	}
}
