# HANDOFF — hookline completion tasks (for a Flash-model agent)

Read this ENTIRE file before touching anything. You are completing a
nearly-finished Go TUI project. Everything is built and 90% green; your job
is the last mile. A senior engineer reviewed the plan below — follow it,
don't redesign.

## Project

`C:\Users\divij\.zcode\workspace\default\hookline` (Git Bash on Windows,
Go module `hookline`). A local-first webhook inspector TUI (capture/inspect/
replay/edit webhooks). Read `docs/ARCHITECTURE.md` for how it fits together.

## Non-negotiable rules

1. `gofmt -w .` before finishing; `go vet ./...` clean; `go test -count=1 ./...` ALL GREEN.
2. NEVER weaken a test to make it pass (no deleting assertions, no skipping).
   If a test encodes OUTDATED expectations (the product intentionally
   changed), update the test with a comment saying what changed and why.
   If it exposes a REAL bug, fix the PRODUCT code minimally.
3. DO NOT touch the core packages `internal/{delivery,listener,store,replay}`:
   they are green and reviewed. UI work happens in `internal/ui` (+ its
   test files). Docs in `README.md` / `docs/`.
4. Match existing code style: doc comments on exported/unexported symbols
   where the file already does that, no TODO comments, no panics, no prints
   in library code.
5. One `git` commit at the very end, only if everything is green (message
   given below). Do not push. Configure nothing; commit with
   `-c user.name="divij" -c user.email="divij@local"`.

## Current state (verified before handoff)

Green: `internal/delivery`, `internal/listener`, `internal/store`,
`internal/replay`, `internal/style` — all pass.
Failing: exactly 5 tests in `internal/ui` (probe files). Diagnoses below.

Recent intentional product changes these probes may predate (do NOT revert):
- Replay carries response body head; toasts colored by status class
  (>=400 bad, >=300 warn); response tab shows last replay; list rows show
  a replay status badge; replay never follows redirects now.
- `dayTime()` renders relative formats (today `15:04:05`, this week
  `Mon 15:04`, older `Jan 02 15:04`) — width VARIES; row math measures it.
- Search (`/`) now also matches HEADERS, not just method/URL/body.
- ctrl+u / ctrl+d / pgup / pgdown page the list (or scroll detail);
  `n` jumps to next unread; `esc` clears filters.
- Help overlay trims rows to fit (`fitHelpRows`) and its panel budget is
  rows + 4 (padding 2 + border 2); `twoColumnFits` budget is widest*2+3+8.
- List pane content is trimmed to pane height; rows truncate to pane width.
- `wrap()` = wordwrap then HARD wrap (unbreakable runs must never overflow).
- Diff view output is wrapped via `st.Wrap`.
- Target input auto-prefixes `http://`; replay to non-loopback target needs
  a second `r` press (confirmRemote guard); target persists via
  `ui.Config.TargetSave` → `config.json` in the data dir.
- Store gained `Rotate(maxKeep)`; `--fresh` rotates instead of destroying.

## The 5 failing tests — run each with `go test -count=1 -run '<Name>' -v ./internal/ui/` first

1. **TestProbeHelpTwoColumnThreshold** — the two-column threshold budget
   changed (+6 → +8 for the border). The probe asserts the OLD crossover
   width. Update the probe to the new formula (see `twoColumnFits`).
2. **TestProbeCtrlUIsClearsLiveSearch** — two possibilities; check the
   -v output: (a) the esc assertion expects the input VALUE to clear;
   preferred fix is product-side: on cancel in `updateInput`, reset
   `m.input` (one line) so prompts never leak text; (b) if it's the
   needle/filter assertion, the probe already uses "aa" — verify the
   haystack change (headers searched) didn't make "aa" ambiguous
   (`application/json` contains no "aa"; check actual haystacks).
3. **TestProbePathTruncationBoundary** — the probe derives the path budget
   dynamically to match `renderRowContent`. If it fails, print both the
   probe's computed budget and the actual rendered row width, find the
   off-by-N, and align the formula. The renderer's budget lives in
   `internal/ui/list.go` `renderRowContent` (measure time/method/meta
   columns exactly as rendered, incl. `methodLabel` padding to 6).
4. **TestProbeEmptyStateNarrowOverflow** — list content is now trimmed to
   pane height; the probe's documented-overflow stubs were replaced. If it
   still fails, read which assertion fires; likely an `assertFrameFits`
   now failing for a NEW reason at 60x15 or 40x10 — fix the real cause in
   `emptyList` (truncate each hint line to the pane width via
   `st.TruncateEnd`, same as the curl example line already does).
5. **TestProbeDiffViewUnwrappedLinesOverflow** — diff output is now
   wrapped; the probe likely asserts the OLD overflow existed. Update it
   to assert the frame fits (`assertFrameFits`) at its terminal size.

If any diagnosis proves wrong, trust the -v output over this file; the
RULES above still apply.

## After green: documentation tasks

### README.md updates (keep it minimal, match existing tone)
- Keys table: add `PgUp/PgDn` (page · ctrl+u/d), `n` (next unread).
- Mention: search covers headers; replay responses shown in the response
  tab with status-colored toasts; replay target persisted to config.json;
  non-loopback targets ask for a confirming second press; `--fresh`
  rotates history to `hookline-<timestamp>.jsonl` (keeps 5).

### docs/FINDINGS.md (NEW file — this is a deliverable the owner asked for)
Structure it exactly as:
1. **Bugs found by the testing campaign & fixed** — list, one bullet each
   with file refs: gzip/JSON-CT misclassification (cURL corruption),
   replay followed redirects silently, torn request bodies persisted,
   unbreakable-token frame overflow (wordwrap-only), tiny-terminal frame
   overflows (header/status/rows/empty-state/help), stale scroll offset
   after filtering, OSC52 race, search allocation storm, esc-in-search
   hint mismatch, mouse events leaking through overlays + X-coordinate
   hit-testing, invalid-UTF-8 cURL corruption, help overlay height
   overflow, detail pane one-row-short, list row off-by-one.
2. **Pragmatic improvements implemented** (from the product review):
   replay response bodies + status-colored toasts + row badges;
   persisted replay target (config.json); non-loopback replay guard;
   date-aware timestamps; PgUp/PgDn + next-unread; header search;
   target visible in status bar; --fresh rotates history; http://
   auto-prefix on target input.
3. **Recommended next (not built)** — prioritized, with the product
   review's rationale condensed: P0 Stripe/GitHub/Shopify signature
   re-signing on replay (`--sign`, `HOOKLINE_SIGN_SECRET`); batch replay
   of filtered set with per-row outcomes; P1 save delivery to file (w);
   load-more history (`--limit`); strip proxy-injected headers on replay;
   method picker overlay; P2 retry simulation with Stripe-like backoff;
   jq-path copy; header editing. Note environment limits found: race
   detector needs CI (no local cgo), Windows loopback SYN limit shapes
   the burst test (waved concurrency).
4. **Verification** section: how everything was proven (test counts,
   e2e capture, probe sweeps across terminal sizes).
Keep the whole file under ~150 lines, factual, no marketing.

## Final commit (only if ALL green + docs done)

```
git add -A && git -c user.name="divij" -c user.email="divij@local" commit -m "checkpoint: hardening pass — campaign fixes + pragmatic improvements

Adversarial-testing campaign findings fixed (classification, redirects,
torn bodies, layout invariants at all sizes); replay closes the loop
(response bodies, badges, persisted target, remote guard); docs/FINDINGS
records bugs, improvements, and the prioritized roadmap."
```

## Verification commands (run in order at the end)

```
cd C:/Users/divij/.zcode/workspace/default/hookline
gofmt -l .            # must print nothing
go vet ./...          # clean
go test -count=1 -timeout 300s ./...   # ALL ok
go build -ldflags "-X main.version=0.2.0" -o bin/hookline.exe ./cmd/hookline
./bin/hookline.exe --version
```
