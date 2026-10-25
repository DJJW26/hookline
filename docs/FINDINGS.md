# FINDINGS — hardening campaign results (v0.2)

Outcome of the adversarial-testing campaign over the v0.1 MVP: bugs found
and fixed, pragmatic improvements shipped, and what is deliberately left
for later. Companion to `docs/REPORT.md` (v0.1 build) and
`docs/ARCHITECTURE.md`.

## 1. Bugs found by the testing campaign & fixed

Every item below is pinned by a regression test (in the package it
belongs to, or in `internal/ui/probe_ui_test.go` / `*_test.go` files).

- **gzip/JSON-CT misclassification corrupted cURL exports** — bodies
  labeled `application/json` but not UTF-8 are classified binary;
  `internal/delivery/delivery.go` (kind sniffing), exercised by
  `internal/replay/curl_test.go`.
- **Replay followed redirects silently** — a 30x chain reported the last
  hop's answer as the target's. The replay client now stops at the first
  response (`CheckRedirect` → `http.ErrUseLastResponse`,
  `internal/replay/replay.go`).
- **Torn request bodies were persisted** — a client aborting mid-body
  stored a truncated payload as if complete; the listener now records the
  read error and marks the body truncated
  (`internal/listener/listener.go`).
- **Unbreakable-token frame overflow** — wordwrap alone passes 2000-char
  tokens through and blows the frame; wrapping is now wordwrap followed by
  a hard wrap (`internal/style/highlight.go`, `Styles.Wrap`).
- **Tiny-terminal frame overflows** — header (`view.go headerLine`),
  status line (`statusLine`), list rows (`renderRow`), the empty-state
  hints (`emptyList` — wide hints grew the panel; now truncated to the
  pane width) and the help overlay all overflowed below ~96×24; every
  band clamps to the terminal now (`internal/ui`).
- **Stale scroll offset after filtering** — filtering while scrolled deep
  rendered a blank list; the offset is clamped in the list render
  (`internal/ui/list.go`).
- **OSC 52 race** — the clipboard flush could fire before the renderer
  painted the sequence; the flush now waits a tick past the ~60 fps
  repaint (`internal/ui/msg.go`, `app.go`).
- **Search allocation storm** — filtering lowercased every body several
  times per frame; search now runs over per-delivery cached haystacks
  (body capped at 64 KiB) and a cached visible list
  (`internal/ui/app.go`).
- **esc-in-search hint mismatch** — the status line promised "esc clear"
  but esc only cancelled; esc in the find prompt now lifts the live
  filter, and cancel leaves no text behind in the prompt widget
  (`internal/ui/app.go` `updateInput`).
- **Mouse events leaked through overlays + X-coordinate hit-testing** —
  clicks/wheels in overlay modes moved the hidden list, and split-layout
  hit-testing ignored which pane was clicked; mouse input is gated to
  normal mode and `listRowAt` respects the pane split
  (`internal/ui/app.go`).
- **Invalid-UTF-8 cURL corruption** — exporting such bodies via cURL
  replaced bytes with U+FFFD; they are treated as binary now
  (`internal/delivery` classification, `internal/replay/curl.go`).
- **Help overlay height overflow** — the single-column help exceeded the
  pane at short heights; rows are trimmed (`fitHelpRows`) and the
  two-column threshold follows the widest row
  (`internal/ui/help.go`).
- **Detail pane one row short** — its height math double-counted the
  title row; exact-frame-height invariant caught it in stacked layouts
  (`internal/ui/detail.go`).
- **List row off-by-one** — the separator budget counted 2 leading
  columns but rendered 3; the budget is now derived from the rendered
  pieces (`internal/ui/list.go` `renderRowContent`).

## 2. Pragmatic improvements implemented (product review)

- **Replay closes the loop**: the target's response head is shown in the
  response tab; toasts are colored by status class (≥400 bad, ≥300 warn);
  list rows carry a replay status badge (`internal/ui/app.go`,
  `detail.go`, `list.go`).
- **Replay target persists** to `config.json` in the data dir
  (`Config.TargetSave` wiring in `internal/ui`, `cmd/hookline/main.go`).
- **Non-loopback replay guard**: the first `r` against a non-loopback
  target only arms a confirming second press
  (`replaySelected`/`isLocalTarget`, `internal/ui/app.go`).
- **Date-aware timestamps**: today renders `15:04:05`, this week
  `Mon 15:04`, older `Jan 02 15:04` (`internal/ui/format.go`).
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
  bounded window; older pages stay on disk.
- **P1 — strip proxy-injected headers on replay** (tunnel/via headers
  from the original capture would poison the resend).
- **P1 — method picker overlay**: the `f` cycle is fine for 5 verbs, weak
  for arbitrary ones.
- **P2 — retry simulation** with Stripe-like backoff schedules (prove the
  handler is idempotent).
- **P2 — jq-path copy** and **header editing** before resend.

Environment limits found during testing: the Go race detector needs CI
(no local cgo on this Windows toolchain), and the Windows loopback SYN
backlog limit shapes the listener burst test — concurrency was waved to
50 sequential-fast connections rather than a hard parallel hammer.

## 4. Verification

- `gofmt -l` clean, `go vet ./...` clean, `go build` + `--version` smoke
  test on the shipped binary.
- **136 test functions** across six packages: listener (concurrent burst,
  byte fidelity, truncation, auto-response), store (round-trip,
  corrupt/torn-line tolerance), replay (byte-identical resend, no
  redirects, cURL quoting), style (highlighting, ANSI-safe wrap/truncate),
  delivery (kind sniffing edges), and `internal/ui` (26 behavior + 39
  adversarial probe tests).
- **End-to-end capture**: the v0.1 binary was validated against a real
  running instance (deliveries, replay round-trip, OSC 52 emission in the
  captured output — see `docs/REPORT.md`).
- **Probe sweeps across terminal sizes**: `TestProbeFrameSweepSizes`
  renders a full frame at 20×6, 40×10, 60×15, 80×24, 120×30 and 200×60,
  asserting the frame-width/height invariant at each; mode-matrix and
  mouse-boundary probes sweep every mode transition and pane edge.
