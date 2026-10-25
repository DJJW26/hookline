# Build report — hookline v0.1 (working MVP)

**Date:** 2026-10-24
**Scope:** Everything in this repository except `SPEC.md` (the input spec).
**Status:** Builds clean, 56 tests green, end-to-end validated against a
real running binary.

## What was built

A complete working MVP of the webhook inspector TUI described in
`SPEC.md`: HTTP capture listener, live two-pane TUI with
syntax-highlighted JSON, replay/edit-and-resend with diff review,
cURL export over OSC 52, search and method filter, JSONL persistence,
help overlay, three themes with auto light/dark detection.

| Layer | Files | What it does |
|---|---|---|
| `cmd/hookline` | 1 | Flags, data dir, wiring, signal handling |
| `internal/delivery` | 1 | The shared `Delivery` record + body-kind sniffing |
| `internal/listener` | 2 (incl. test) | Capture server: any method/path, 10 MiB cap, auto-response |
| `internal/store` | 2 | Crash-safe JSONL persistence, 2 MiB stored-body cap |
| `internal/replay` | 4 | Byte-faithful resend + shell-safe cURL export |
| `internal/style` | 6 | Theme registry, Dusk/Linen/Graphite palettes, JSON highlighter, hexdump, ANSI-aware wrap |
| `internal/ui` | 21 | The Bubble Tea app: panes, modes, input, help, diff, clipboard, editor |

**28 Go files · 3,223 source lines · 1,556 test lines · 4 direct
dependencies** (bubbletea, lipgloss, reflow, termenv — all Charm-ecosystem
stable v1). No widget library: the input widget and list renderer are
hand-rolled to keep the dependency tree and visual behavior fully ours.

## How it was built (orchestration)

Three-phase build with subagents under supervision:

1. **Design research agent** cloned Charm's `crush` and produced a
   design-language report (themes, idioms, terminal integration). Its
   findings were applied as *inspiration only* — crush is FSL-1.1-MIT
   licensed, so no code, themes or assets were copied; only colour-role
   ideas and interaction idioms informed our own original palettes.
2. **Core-implementation agent** built the three stdlib-only core packages
   against a delivery-type contract written first (so the UI and core
   were built against the same interface in parallel). Its code was then
   reviewed line-by-line and integrated; its deviations (Windows-safe
   `Clear`, `EscapedPath` fidelity, compression-disabled replay client)
   were verified and kept.
3. **Everything else** (style system, UI, main, tests, docs) was built
   directly, followed by an **independent read-only review agent** whose
   findings were triaged and fixed (see "Review outcomes" below).

## Design decisions

### Look and feel
- **From Crush**: the `#6B50FF` purple accent family, the calm text ladder
  (`#ECEBF0 / #858392 / #605F6B`), rounded borders as the single panel
  shape, focus indicated by border brightening, TTL toasts over the
  status line.
- **From Claude Code**: minimal chrome (one-line header, one-line status),
  muted hints, generous restraint — one accent used sparingly, a second
  (warm `#FF985A`) reserved solely for the live indicator.
- **Deliberate departure from both**: hookline never paints its own
  background. Panels sit directly on the user's terminal canvas so the
  app inherits whatever theme, opacity, or wallpaper the user configured
  — the core of the "blends into your terminal" requirement.

### Terminal blending (the customization surface)
Transparent backgrounds · auto dark/light via terminal background query ·
truecolor → 256 → 16 → mono degradation · `NO_COLOR`/`CLICOLOR_FORCE`
respected · OSC 52 clipboard (works over SSH; `HOOKLINE_CLIP_CMD`
fallback) · OSC 0/2 window title · altscreen + mouse cell-motion ·
responsive split/stack at 96 columns · UTF-8-only glyphs (no nerd fonts).
Verified live: the running binary emits all expected sequences (visible
in the e2e capture log).

### Bubble Tea v1, not v2
crush pins the v2 generation; hookline deliberately stays on stable v1 —
the most battle-tested API across the widest set of terminals. All tea
usage lives in `internal/ui`, so a later migration is one package deep.

## Bugs found and fixed during development

An honest log — every one of these was caught by the test suite, a
targeted probe, or review:

1. **Input-mode escape**: pressing Enter on the replay-target prompt set
   the target but returned before leaving input mode, so the next `r`
   was typed into the prompt and returned a nil command (test panic).
   Fixed by restructuring `updateInput` around a single exit path.
2. **List row off-by-one**: the separator budget counted 2 leading
   columns but rendered 3 (`" mark "`), making every row 1 column too
   wide — caught by the frame-width invariant test, fixed by deriving
   the budget from the rendered pieces.
3. **Detail pane one row short**: its height math subtracted a title row
   that was already inside the content; invisible in split layout (the
   list pane set the height) but broken in stacked layout. Found by the
   exact-frame-height invariant at 80×24.
4. **Help overlay overflow**: the single-column help panel exceeded the
   pane height at 24 rows; rewritten as a two-column layout with a
   narrow-terminal fallback.
5. **Broken `NewID` fallback** in the delivery contract (unreachable
   formatting) — caught during self-review before integration.
6. **Test-only**: lipgloss ignores renderer-level forced color profiles;
   tests now force the profile globally (`SetColorProfile`), and one
   wrong expectation about `json.Indent` spacing was corrected.
7. **Post-build self-review**: removed an unused `toast.at` field and an
   O(n²) selection lookup inside the row-render loop.

## Review outcomes

The independent read-only review (verified against bubbletea v1.3.10
source where relevant) returned **7 bugs, 5 uniformity violations, 10
bloat items**, verdict FIX-FIRST. All were fixed:

- **OSC 52 clipboard race** — the flush message could clear the escape
  sequence before the renderer's ~60fps ticker ever painted it; the flush
  now fires on an 80 ms tick so the sequence is guaranteed painted.
- **Blank list after filtering while scrolled deep** — the scroll offset
  is now clamped in the list render.
- **Per-frame allocation storm** — search lowercased entire bodies of
  every delivery several times per frame; filtering now runs over a
  cached visible-list plus per-delivery cached haystacks (body capped at
  64 KiB), recomputed only when items or filters change.
- **`esc` in the find prompt now clears the filter**, matching the hint.
- **Mouse events are gated to normal mode** and click hit-testing respects
  the pane layout (X coordinate in split mode, correct row bounds) —
  clicks in overlays or the detail pane no longer move the list.
- **Invalid-UTF-8 bodies are classified binary** — cURL export can no
  longer corrupt them via U+FFFD substitution.
- **Big JSON bodies** are indented once (cached) and only the displayed
  head is colourised, so truncation no longer breaks highlighting.
- **Uniformity**: selected-row backgrounds moved into the style sheet
  (`RowSelected`/`RowSelectedLive`), the UI no longer knows builtin theme
  IDs or the listener's response body, and the keys comment no longer
  overclaims.
- **Bloat removed**: dead `FormValues`, `confirmKind`, `listState.live`,
  `inputModel.width` (now wired), `toast.at`, `Theme.Dark`, `Styles.Row`,
  `cacheLines`, the redundant SIGINT goroutine (bubbletea installs its
  own handlers), and the stray `bin/` artifact.
- Also from the review's nice-to-haves: in-memory sessions are capped at
  256 MiB of bodies (oldest evicted; disk history unaffected), and
  `--bind 0.0.0.0` now displays `localhost` in URLs.

Six regression tests cover the fixed bugs (deep-scroll filter, esc-clear,
overlay mouse gate, pane-aware clicks, OSC emission, body search). One
accepted simplification, noted with the reviewer: every arriving delivery
is marked unread, not only those arriving while scrolled away (SPEC §F2
loosened accordingly).

## Verification

- `gofmt -l` clean, `go vet ./...` clean.
- `go test -count=1 ./...` — all packages green, **56 test functions**:
  - listener: 50-request concurrent burst, byte-identical bodies, headers,
    query fidelity, truncation caps, auto-response contract
  - store: round-trip, corrupt/torn-line tolerance, truncation, clear,
    Windows-safe reopen
  - replay: byte-identical resend incl. hop-by-hop header stripping,
    cURL quoting (embedded quotes), 4xx-not-error semantics
  - style: highlighting fidelity, ANSI-safe wrapping/truncation, theme
    registry, mono degrades to no-color
  - ui: model-level reducer/renderer tests incl. layout invariants
    (every line ≤ width; frame height exactly matches terminal) across
    split, stacked, help, and diff modes; navigation, filtering, replay
    round-trip against an `httptest` server, editor→diff flow, plus six
    review-regression tests
- **End-to-end against the real binary**: ran the compiled exe, POSTed a
  genuine curl webhook with custom headers and query params mid-run,
  confirmed the auto-response, the window-title/mouse/altscreen escape
  sequences, and the persisted JSONL record (id, headers, base64 body).
- **Race detector**: not runnable on this Windows box (no cgo/gcc);
  concurrency is designed safe (Bubble Tea serializes `Update`; store is
  mutex-guarded; listener relies on `net/http` semantics) and `-race`
  belongs in CI (next step below).

## Deviations from SPEC.md

- **Module path** is `hookline` (SPEC §12 left it to the maintainer);
  rename to `github.com/<user>/hookline` before publishing.
- **teatest → model-level UI tests**: deterministic, dependency-free,
  same reducer/renderer coverage; documented in ARCHITECTURE.md.
- **huh dropped**: the two prompts use a hand-rolled input, removing a
  large dependency tree for two text fields.
- **M4 release tooling** (goreleaser, CI, VHS GIF) intentionally deferred
  — see next steps.

## Next steps (in order)

1. GitHub Actions: lint + test matrix (windows/ubuntu/macos) with
   `-race`, goreleaser on tags.
2. VHS tape (`.vhs/demo.tape`) → README GIF — the launch asset.
3. Rename module path, publish, add Homebrew/Scoop.
4. Roadmap features from SPEC §5, starting with the built-in Cloudflare
   quick tunnel.

---

## Addendum (v0.1.1)

An adversarial-testing hardening pass followed v0.1.0: the bugs fixed
and improvements shipped are recorded in `docs/FINDINGS.md` (replay now
keeps the target's response head and never follows redirects; torn
captures are refused; layout is overflow-proof at any size; `--fresh`
rotates history). The suite is now 136 test functions across six
packages. This report reflects the v0.1.0 build and is left unedited
above.
