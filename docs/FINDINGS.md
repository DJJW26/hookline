# FINDINGS — hardening campaign results (v0.2)

Outcome of the adversarial-testing campaign over the v0.1 MVP: bugs found
and fixed, improvements shipped, what is left for later. Companion to
`docs/REPORT.md` (v0.1 build) and `docs/ARCHITECTURE.md`.

## 1. Bugs found by the testing campaign & fixed

Each item names the fix site and the test exercising it (the frame
invariants live in `assertFrameFits`, `internal/ui/app_test.go`).

- **gzip/JSON-CT misclassification corrupted cURL exports** — a gzip body
  labeled `application/json` was inlined into the cURL command; binary
  bytes now beat the declared Content-Type in `Delivery.Kind`
  (`internal/delivery/delivery.go`), pinned by the delivery probe
  ("json ct with gzip magic is binary").
- **Replay followed redirects silently** — a 30x chain reported the last
  hop's answer as the target's; the client now stops at the first response
  (`CheckRedirect` → `http.ErrUseLastResponse`,
  `internal/replay/replay.go`), and `TestDoRedirectBehavior` logs a
  FINDING if a redirect is ever followed again.
- **Torn request bodies were persisted** — a client aborting mid-body
  stored a partial payload as if complete; the handler now refuses the
  capture with a 400 instead of recording it
  (`internal/listener/listener.go`), asserted by
  `TestProbeListenerStopWhileRequestInFlight` (want zero captures).
- **Unbreakable-token frame overflow** — wordwrap alone passes 2000-char
  tokens through and blows the frame; wrapping is wordwrap followed by a
  hard wrap (`internal/style/highlight.go`, `Styles.Wrap`), pinned by
  `TestWrapBreaksUnbreakableRuns`.
- **Tiny-terminal frame overflows** — header (`view.go headerLine`),
  status line (`statusLine`), list rows (`renderRow`), the empty-state
  hints (`emptyList`, now truncated to the pane width) and the help
  overlay all overflowed below ~96×24; every band clamps to the terminal
  now (`internal/ui`), pinned by `TestProbeFrameSweepSizes` and
  `TestProbeEmptyStateNarrowOverflow`.
- **Stale scroll offset after filtering** — filtering while scrolled deep
  rendered a blank list; the offset is clamped in the list render
  (`internal/ui/list.go`), pinned by
  `TestFilterAfterDeepScrollDoesNotBlankList`.
- **OSC 52 race** — the clipboard flush could fire before the renderer
  painted the sequence; the flush now waits 80 ms, comfortably past the
  ~60 fps repaint (`internal/ui/msg.go`, `app.go`), pinned by
  `TestCopyEmitsOSC52ViaView`.
- **Search allocation storm** — filtering lowercased every body several
  times per frame; search now runs over per-delivery cached haystacks
  (body capped at 64 KiB) and a cached visible list
  (`internal/ui/app.go`), pinned by `TestSearchMatchesBodyWithinCap` and
  `TestProbeClearThenBurstRebuildsCaches`.
- **esc-in-search hint mismatch** — the status line promised "esc clear"
  but esc only cancelled; esc in the find prompt now lifts the live
  filter, and cancel leaves no text behind in the prompt widget
  (`internal/ui/app.go` `updateInput`), pinned by
  `TestSearchEscClearsFilter` + `TestProbeCtrlUIsClearsLiveSearch`.
- **Mouse events leaked through overlays + X-coordinate hit-testing** —
  clicks/wheels in overlay modes moved the hidden list and split-layout
  hit-testing ignored which pane was clicked; mouse input is gated to
  normal mode and `listRowAt` respects the pane split
  (`internal/ui/app.go`), pinned by `TestMouseIgnoredInOverlayModes` and
  the click-boundary probes.
- **Invalid-UTF-8 cURL corruption** — exporting such bodies via cURL
  replaced bytes with U+FFFD; they are classified binary now
  (`internal/delivery` sniffing, `internal/replay/curl.go`), pinned by
  `TestKindInvalidUTF8IsBinary` and `TestToCurlInvalidUTF8TreatedAsBinary`.
- **Help overlay height overflow** — the single-column help exceeded the
  pane at short heights; rows are trimmed (`fitHelpRows`) and the
  two-column threshold follows the widest row (`internal/ui/help.go`),
  pinned by `TestProbeResizeTransitionsMidMode` +
  `TestProbeHelpTwoColumnThreshold`.
- **Detail pane one row short** — its height math double-counted the
  title row; content is now exactly h-2 rows inside the border
  (`internal/ui/detail.go`), caught by the exact-frame-height invariant
  (`TestNarrowLayoutStacks`).
- **List row off-by-one** — the separator budget counted 2 leading
  columns but rendered 3; the budget is now derived from the rendered
  pieces, measuring the variable-width time column
  (`internal/ui/list.go` `renderRowContent`), pinned by
  `TestProbePathTruncationBoundary`.

## 2. Pragmatic improvements implemented (product review)

- **Replay closes the loop**: the target's response head (≤16 KiB,
  `internal/replay/replay.go`) is shown in the response tab; toasts are
  colored by status class (≥400 bad, ≥300 warn); list rows carry a replay
  status badge (`internal/ui/app.go`, `detail.go`, `list.go`).
- **Replay target persists** to `config.json` in the data dir
  (`Config.TargetSave` wiring in `internal/ui/app.go`,
  `cmd/hookline/main.go`).
- **Non-loopback replay guard**: the first `r` against a non-loopback
  target only arms a confirming second press
  (`replaySelected`/`isLocalTarget`, `internal/ui/app.go`).
- **Date-aware timestamps**: today renders `15:04:05`, this week
  `Mon 15:04`, older `Jan 02 15:04` (`internal/ui/format.go` `dayTime`).
- **Paging & unread flow**: PgUp/PgDn / ctrl+u/d page the list or scroll
  the detail; `n` jumps to the next unread delivery (`internal/ui`).
- **Header search**: `/` matches headers, not just method/URL/body
  (`internal/ui/app.go` `haystack`).
- **Target visible in the status bar** — the right side always shows
  where `r` would send (`internal/ui/view.go`).
- **`--fresh` rotates** history to `hookline-<timestamp>.jsonl` (keeps 5)
  instead of destroying it (`cmd/hookline/main.go`,
  `internal/store/store.go` `Rotate`).
- **`http://` auto-prefix** on the target input, so `localhost:3000`
  works (`normalizeTarget`, `internal/ui/app.go`).

## 3. Recommended next (not built)

- **P0 — signature re-signing on replay**: Stripe/GitHub/Shopify verify
  signatures, so a replayed capture is rejected as tampered. Add `--sign`
  per provider (HMAC-SHA256 with `HOOKLINE_SIGN_SECRET`) recomputed on
  resend; without it the replay loop stays open against those providers.
- **P0 — batch replay of the filtered set** with per-row outcomes: the
  "re-send all 40 failed webhooks after I fix the server" workflow needs
  fan-out plus a visible pass/fail per row.
- **P1 — save delivery to file (`w`)** for attaching to bug reports.
- **P1 — load-more history (`--limit`)**: today the session loads a
  bounded window (`LoadLast(200)`); older pages stay on disk.
- **P1 — strip proxy-injected headers on replay** (tunnel/via headers
  from the original capture would poison the resend).
- **P1 — method picker overlay**: the `f` cycle is fine for 5 verbs, weak
  for arbitrary ones.
- **P2 — retry simulation** with Stripe-like backoff (idempotency check).
- **P2 — jq-path copy** and **header editing** before resend.

Environment limits found during testing: `go test -race` is not runnable
here (`-race requires cgo`; no local C compiler) so it belongs in CI; and
the Windows loopback SYN backlog shapes the burst probe — 500 requests
fire in waves of 50 concurrent (500 simultaneously-dialed loopback
connections can trip OS SYN handling; `TestProbeListenerBurst500`).

## 4. Verification

- `gofmt -l .` prints nothing; `go vet ./...` is clean; a full build
  with `-ldflags "-X main.version=0.2.0"` succeeds and `--version`
  prints `hookline 0.2.0`.
- `go test -count=1 -timeout 300s ./...` — all six packages ok: **136
  test functions** (8/17/15/15/16/65); `internal/ui` is 26 behavior
  tests plus 39 adversarial probes.
- **End-to-end capture (live, v0.2.0 binary)**: ran `bin/hookline.exe`
  with a throwaway `HOOKLINE_DATA_DIR`, POSTed a JSON webhook with a
  custom header mid-run — auto-response 200, and the JSONL record landed
  with id, timestamp, headers, base64 body, source IP and response
  status; the TUI emitted its altscreen/mouse/title sequences on boot.
- `TestDoRedirectBehavior -v` reports `redirect handling: reported
  status 302` with no FINDING log — the no-redirect fix holds.
- **Probe sweeps across terminal sizes**: `TestProbeFrameSweepSizes`
  renders a full frame at 20×6, 40×10, 60×15, 80×24, 120×30 and 200×60,
  asserting the frame-width/height invariant at each; the mode-matrix and
  mouse-boundary probes sweep every mode transition and pane edge.
