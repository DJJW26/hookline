# How hookline works

This document explains the project's structure, data flow, and the
decisions behind them — read it before changing anything.

## The one rule

```
cmd/hookline ──▶ internal/ui ──▶ internal/style
                    │
                    ▼
        internal/delivery  ◀── internal/listener
                             internal/store
                             internal/replay
```

**Dependencies point one way: UI → core.** The core packages
(`delivery`, `listener`, `store`, `replay`) are pure Go on the standard
library — they never import Bubble Tea, Lip Gloss, or each other's
internals. Everything they need to exchange is a `delivery.Delivery`.
This is what keeps the logic testable without a terminal, and what makes
a future `--json` headless mode nearly free: the UI is one consumer of
the core, not the other way round.

## Packages

| Package | Responsibility | Notes |
|---|---|---|
| `internal/delivery` | The shared `Delivery` record + body-kind sniffing | The contract type; bodies are `[]byte` (base64 in JSON) so binary round-trips |
| `internal/listener` | HTTP capture server | Any method/path; 10 MiB body cap; replies `--status` (default 200) with an `X-Hookline-ID` header |
| `internal/store` | JSONL persistence | Append-only, crash-safe (torn lines skipped on read), 2 MiB stored-body cap |
| `internal/replay` | Resend + cURL export | Strips hop-by-hop headers; `DisableCompression` client so `Accept-Encoding` isn't re-added; shell-safe quoting |
| `internal/style` | Every color, border, glyph | Theme registry (`dark`/`light`/`mono`) + `Styles` sheet; UI never builds a raw style |
| `internal/ui` | The Bubble Tea app | Elm-architecture model; owns layout, keys, modes |
| `cmd/hookline` | Wiring | Flags → theme/data-dir → store → listener → program |

## Data flow of one webhook

1. `cmd/hookline` starts the listener with an `OnDelivery` callback.
2. A POST arrives. The handler reads up to 10 MiB of body, clones the
   headers, stamps an ID (`unixmillis-<rand hex>`) and builds a
   `Delivery`. It replies immediately, *then* invokes the callback.
3. The callback does two things, in order: `store.Append` (flush-to-disk
   before anything else — a crash never loses a capture) and a
   non-blocking push into a 1024-slot channel (drop instead of blocking
   if the UI has exited).
4. The UI's `waitForDelivery` command — re-armed after every message —
   turns the channel read into a `deliveryMsg`. `Update` prepends it to
   the model (newest first), marks it unread, re-arms the pump.
5. `View` renders the frame. Rendering is cached per
   `(delivery ID, tab, width)` so a 5 MiB body is tokenized once, and
   only its first 100 KiB is ever rendered per frame.

## The UI model

`internal/ui` is one Bubble Tea model (`Model` in `app.go`) plus small
sub-models, following the Elm architecture: messages in, new state out,
`View` is a pure function of state.

- **Modes** (`msg.go`): `normal`, `search`, `target`, `diff`, `confirm`,
  `help`. Input modes swallow keys first; every mode has an escape path.
- **Focus** travels between two panes (`list`, `detail`) with `tab`;
  the focused pane's border brightens (the Crush idiom) and the status
  line's hints change to match.
- **Selection is by delivery ID, not index** (`list.go`): a burst of new
  deliveries shifts every index, but never yanks the selection.
- **Layout** (`view.go`): four bands — header (wordmark left, listener
  state right), pane area (side-by-side ≥ 96 columns, stacked below),
  input row (reserved even when hidden, so opening a prompt never jumps
  the frame), status line (hints or a 4-second toast). Two invariants
  hold at every size and are enforced by tests: every line fits the
  terminal width, and the frame is exactly the terminal height.
- **Toasts** replace the status hints and clear via `tea.Tick`.
- **Mouse**: wheel scrolls the focused pane; a click maps a screen row
  back to a delivery via the same layout math the renderer uses.

### The edit-and-resend loop

`e` writes the body to a temp file and suspends the TUI with
`tea.ExecProcess` while `$EDITOR` runs (`notepad` on Windows, `vi`
elsewhere). The edited file comes back as an `editorDoneMsg`; if it
differs, the app enters `diff` mode — a theme-colored LCS line diff —
where `s` replays the edited copy (with a fresh ID) and `esc` discards.

## The theme system

`internal/style/theme.go` holds a registry of `Theme` values — semantic
color roles (Base/Dim/Faint, Accent/Accent2, Good/Warn/Bad, borders,
surfaces, per-method colors, JSON token colors). `style.New(theme)`
compiles a theme into a `Styles` sheet of ready `lipgloss.Style` values;
the UI receives the sheet and never touches raw colors. Adding a theme =
registering one struct; future config-file themes plug into the same
`Register` call.

Palette lineage: the default dark theme (**Dusk**) borrows Crush's
Charmtone Pantera purple (`#6B50FF` family) and its calm text ladder
(`#ECEBF0/#858392/#605F6B`); Claude Code contributes the restraint —
muted status line, minimal chrome, one accent used sparingly.
Unlike Crush, hookline never paints its own background: panels sit
directly on the user's terminal canvas, so your terminal theme, opacity
and wallpaper show through.

## Terminal blending

hookline deliberately uses the terminal's own customization surfaces
instead of fighting them:

- **Transparent backgrounds** everywhere; only selected rows get a
  subtle surface tint.
- **Dark/light auto-detection** (`--theme auto`) queries the terminal
  background and picks Dusk or Linen.
- **Color degradation** through Lip Gloss/termenv: truecolor → 256 →
  16 → mono, with `NO_COLOR` and `CLICOLOR_FORCE` respected. The
  `mono` theme degrades to weight-only hierarchy for minimal terminals.
- **OSC 52 clipboard** — the terminal itself performs the copy, which is
  why it works identically locally and over SSH; `HOOKLINE_CLIP_CMD` is
  the escape hatch for terminals without it.
- **OSC 0/2 window title** set on start (`hookline — 127.0.0.1:4040`).
- **Altscreen + mouse cell-motion**, both standard and universally
  supported; UTF-8 glyphs only (◆ ⏺ • · …), no nerd-font dependencies.
- **Responsive layout** on `SIGWINCH`/resize: panes split at ≥ 96
  columns and stack below; the frame invariants hold at 80×24.

## Persistence format

One JSON object per line in `hookline.jsonl` (bodies base64 via
`encoding/json`'s native `[]byte` handling):

```json
{"id":"1792864078120-b13cd17867ae","ts":"2026-10-24T23:17:58+05:30",
 "method":"POST","path":"/e2e/hook","query":"src=test",
 "headers":{"Content-Type":["application/json"]},
 "body_b64":"eyJldmVudCI6InBheW1lbnQuc3VjY2VlZGVkIn0=",
 "src_ip":"127.0.0.1","response_status":200}
```

Appends flush per line; `LoadLast(n)` scans the file through a ring
buffer and skips any corrupt/torn line, so a crash mid-write costs at
most one delivery. On Windows, `Clear` closes the handle before
truncating (append-mode handles deny truncation there).

## Testing strategy

- **Core packages**: table-driven tests against `httptest` servers and
  temp dirs — a 50-request concurrent burst, byte-identical replay
  assertions, corrupt-tail reads, truncation caps.
- **UI**: model-level tests (`internal/ui/app_test.go`) drive `Update`
  with real messages and assert on `View` output — same reducer, same
  renderer as production, no terminal required, fully deterministic.
  The layout invariants (width fit, exact frame height) are asserted at
  every layout mode.
- **Style**: highlighting, wrapping and truncation are tested with a
  forced truecolor profile; the mono theme is tested for zero color.
- **Race detector**: `go test -race` is the CI's job (needs cgo, not
  present on every dev box); the design keeps the UI single-goroutine
  (Bubble Tea serializes `Update`) and the store mutex-protected.

## Extension points

- **New theme**: add a `Theme` value + `Register` call in
  `internal/style/theme.go`. Nothing else changes.
- **New detail tab**: add to `detailTabs`, render in
  `detail.renderBody`, wire a key in `keys.go` help table.
- **New core consumer** (e.g. `--json` mode): it already exists as a
  plain library — call `listener.Start`, range over deliveries, skip
  the UI entirely.
- **Built-in tunnel (roadmap)**: a new `internal/tunnel` core package
  following the same rules; surface the public URL through
  `ui.Config.ListenURL`.

## Deliberate limitations (v0.1)

Bubble Tea **v1** is pinned, not v2: v1 is the maximally compatible,
stable API across every terminal we target; the UI layer contains all
tea usage, so a future v2 migration is one package deep. The in-TUI
diff editor, delivery-vs-delivery diffing, and multi-session support
are roadmap, not absent by accident — see `SPEC.md` §5.
