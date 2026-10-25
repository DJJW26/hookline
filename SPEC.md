# Hookline — Webhook Inspector TUI — Implementation Spec

**Status:** v1, implemented (working MVP in this repository); hardened in
v0.1.1 — see `docs/FINDINGS.md`
**Name:** **Hookline** (decided 2026-10-24; folder, module and binary all `hookline`)
**Stack:** Go + Charm (Bubble Tea, Lip Gloss, Bubbles, Huh)

> This document is self-contained. An implementing agent needs nothing beyond
> this file. Build to the MVP in §4, following the architecture rules in §6
> and the engineering bar in §9.

---

## 1. Summary

A local-first, open-source terminal UI for **capturing, inspecting, replaying,
and editing inbound webhooks** (Stripe, GitHub, Shopify, Slack, etc.).

One static binary. Run it → it starts an HTTP listener and a TUI showing every
inbound request live. Select a delivery to view syntax-highlighted JSON,
headers, and timing. Replay it to your dev server, edit the payload and resend,
or export it as a cURL command. Nothing leaves the machine unless the user
presses replay.

**Elevator pitch for the README:**
> webhook.site and RequestBin, but in your terminal, offline, with no signup —
> and you can replay and edit deliveries without touching the mouse.

## 2. Why this exists (positioning)

- Webhook debugging is a universal backend pain: providers retry, payloads are
  opaque, and "what exactly did they send me?" is hard to answer.
- Existing tools are web UIs (webhook.site, RequestBin) or SaaS CLIs
  (Hookdeck). As of Oct 2026 there is **no polished, local, open-source TUI**
  in this niche.
- Differentiators to protect in every design decision:
  local-only privacy · zero signup/account · keyboard-fast · works over SSH ·
  single static binary, CGO-free.

## 3. Goals and non-goals

**MVP goals**
1. Capture every inbound HTTP request (any method, path, content-type).
2. Live list + rich detail view, usable at 80×24.
3. Replay and edit-and-resend.
4. cURL export that works over SSH (OSC52).
5. Search/filter, persistence across restarts, in-app help.
6. Cross-platform: Windows Terminal, macOS, Linux — `go install` + release
   binaries.

**Non-goals for v1** (do not build, even if easy)
- No cloud/SaaS component, no accounts, no telemetry.
- No WebSocket/gRPC capture (roadmap only).
- Not a general API client (Postman-alikes exist; stay on webhooks).
- No payload forwarding to third-party services.

## 4. Feature spec (MVP)

### F1 — HTTP capture engine
- Listener on a configurable port (`--port`, default `4040`), binds `127.0.0.1`
  by default; `--bind 0.0.0.0` opt-in.
- Accepts **any** method, **any** path + query, **any** content-type — the
  point is to record what arrived, including routing mistakes.
- Records per delivery: timestamp, method, path + query, all headers, raw body
  bytes, source IP, and the response status we returned (auto-`200`; `--status`
  flag to change).
- Bursts must not drop deliveries: the handler enqueues into a buffered
  channel; the UI consumes asynchronously. Test with 50 rapid requests.

### F2 — Request list (main pane)
- `bubbles/table`: time, method, path, size, content-type (short label).
- Newest first. Unread marker (e.g. `•`) on rows that arrived while the user
  was scrolled away or focused on the detail pane.
- Live updates must not steal focus or reset scroll position.

### F3 — Detail view (secondary pane, `enter`/`tab` to focus)
Tabs: `Body` · `Headers` · `Raw` · `Response`.
- **Body:** pretty-printed JSON with syntax highlighting when content-type is
  JSON (sniff the body too — providers lie); `application/x-www-form-urlencoded`
  rendered as a key/value table; anything binary → hex dump of the first 4 KB.
  Bodies > 100 KB render truncated with a "showing first 100 KB of N" notice —
  the UI must never freeze on a 5 MB payload (test this).
- **Headers:** aligned key/value list.
- **Raw:** exact wire bytes as received.
- **Response:** what our listener replied (status + body).

### F4 — Replay
- `r` on the selected delivery resends the **exact raw bytes and headers** to a
  target URL. Default target `http://localhost:3000`, settable via `--target`
  or in-TUI via `t` (Huh form, remembers last value).
- Result (status code, latency) shown as a transient status-line toast.
- `R` = replay last received delivery without leaving the detail pane.

### F5 — Edit & resend
- `e` opens the body in `$EDITOR` (`notepad` fallback on Windows).
- On save: show a colored diff (added/removed lines), then prompt
  send/cancel. In-TUI textarea editing is v0.2 — do not build it now.

### F6 — cURL export
- `c` copies a ready-to-paste `curl` command (method, headers, body) to the
  clipboard using **OSC52** so it also works over SSH; clear fallback message
  if the terminal refuses.
- `y` copies the raw body only.

### F7 — Search & filter
- `/` substring search across method + path + body.
- `f` filter by method. Active filter always visible in the status bar;
  `esc` clears.

### F8 — Persistence
- History as JSONL in the OS user data dir (`%LOCALAPPDATA%\Hookline` on
  Windows, `$XDG_DATA_HOME/hookline` elsewhere). One line per delivery.
- Loads the most recent 200 on start. `--fresh` starts clean; `x` clears the
  visible list (with confirm).

### F9 — Help & first run
- `?` toggles a keybinding cheatsheet overlay.
- Empty state must be *useful*: show a copyable sample
  `curl -X POST http://localhost:4040/test -d '{"hello":"world"}'` so the user
  can see the tool work in 10 seconds.

### Definition of done (MVP acceptance)
- [ ] Runs on Windows Terminal, macOS Terminal, and a Linux SSH session.
- [ ] 50-request burst: all captured, UI stays responsive.
- [ ] 5 MB JSON body: no freeze, truncated render with notice.
- [ ] Resize to 80×24 and back: layout recovers, no panic.
- [ ] `go test -race ./...` green; UI tests via `teatest` (see §9).
- [ ] Replay against an `httptest` server sends byte-identical body + headers.
- [ ] Clean Ctrl+C shutdown flushes the store; no truncated last line.

## 5. Roadmap (post-MVP, do not build now)

- `--tunnel cloudflare`: built-in Cloudflare quick tunnel (no account) with the
  public URL shown in the status bar — the #1 requested workflow.
- Side-by-side diff of two deliveries (Stripe retry comparison is a killer demo).
- In-TUI body editor (`bubbles/textarea`) replacing the $EDITOR round-trip.
- Secret highlighting / header redaction profiles.
- Named sessions (multiple parallel listeners).
- `--json` headless mode (dump deliveries to stdout; trivial once §6 is respected).
- Terminal bell notification on arrival.

## 6. Architecture

```
cmd/hookline/main.go      flags, wiring, program lifecycle
internal/listener/        HTTP server, capture, event queue   (no UI deps)
internal/store/           JSONL persistence, session model    (no UI deps)
internal/replay/          resend logic, diffing               (no UI deps)
internal/ui/              Bubble Tea models: app, list, detail, help, keys
internal/style/           Lip Gloss styles, theme, adaptive colors
```

**The one rule that matters:** `internal/ui` may import the core packages;
core packages **never** import `ui` or `bubbletea`. Core is a plain Go
library — this is what later enables `--json` mode, and what makes the logic
unit-testable without a terminal.

Event flow: `listener` pushes captures on a channel → a Bubble Tea `Cmd`
goroutine wraps the channel receive → `Update` appends to the model and
requests a store flush.

Dependencies (all pure-Go / CGO-free so goreleaser cross-compiles trivially):
`bubbletea`, `bubbles`, `lipgloss`, `huh`. Nothing else without justification.

Body rendering: pretty-print with `encoding/json`, tokenize, color with
Lip Gloss styles. No heavyweight highlighting library in v1.

## 7. UX specification

```
┌ Hookline ────────────────────────────────── listening :4040 · 12 reqs ┐
│ TIME  METHOD  PATH                    SIZE   TYPE                      │
│ 14:02 POST    /hooks/github           1.2KB  json                      │
│ 14:01 POST    /stripe                 812B   json                      │
│ 13:58 GET     /health                 —      —                         │
├───────────────────────────────────────────────────────────────────────┤
│ BODY │ HEADERS │ RAW │ RESPONSE                                        │
│ {                                                                       │
│   "action": "opened",                                    ← detail pane  │
│   "pull_request": { … }                                                 │
├───────────────────────────────────────────────────────────────────────┤
│ ↑/↓ select · tab focus · r replay · e edit · c cURL · ? help          │
└───────────────────────────────────────────────────────────────────────┘
```

- Split layout: list left (or top at narrow widths — make it responsive),
  detail right/bottom, status bar always visible.
- Method colors follow HTTP convention: GET blue · POST green · PUT yellow ·
  DELETE red · PATCH magenta.
- Full mouse support (click to select, wheel to scroll) **and** full keyboard
  support; neither is optional.

| Key | Action |
|---|---|
| `tab` / `shift-tab` | move focus between panes |
| `↑↓` / `jk` | navigate list |
| `enter` | open delivery in focused detail / fullscreen detail |
| `h l` or `1-4` | switch detail tabs |
| `r` / `R` | replay selected / replay latest |
| `e` | edit body in $EDITOR, diff, resend |
| `c` / `y` | copy as cURL / copy raw body |
| `/` / `f` | search / filter by method |
| `t` | set replay target |
| `x` | clear list (confirm) |
| `?` | help overlay · `q` quit |

## 8. On-disk format

One JSON object per line, `body_b64` always (binary-safe), bodies capped at
2 MB persisted:

```json
{"id":"…","ts":"2026-10-24T14:02:11Z","method":"POST","path":"/hooks/github",
 "query":"","headers":{"content-type":["application/json"]},
 "body_b64":"…","src_ip":"127.0.0.1","response_status":200,"duration_ms":0}
```

## 9. Engineering bar

- Go 1.23+. `golangci-lint` (with `gofumpt`) clean; `go vet` clean.
- Core packages: table-driven tests (`httptest` for listener/replay, round-trip
  test for store). Race detector on in CI.
- UI: `teatest` covering at minimum — boot into empty state, list selection
  updates the detail pane, filter applies, help overlay toggles. Deterministic
  UI tests are a differentiator in this ecosystem; treat them as a feature.
- GitHub Actions: lint + test matrix (windows-latest, ubuntu-latest,
  macos-latest); goreleaser on tag.
- Version via `-ldflags "-X main.version=…"`; support `--version`.
- Ctrl+C / SIGTERM: flush store, close listener cleanly.
- No network calls except user-initiated replay. No telemetry. Ever.

## 10. Docs & launch

- README opens with a **VHS-recorded GIF** (keep the `.tape` file in `repo/.vhs/`)
  showing: start → curl a fake Stripe webhook → JSON lights up → replay.
- Install: `go install` one-liner, goreleaser binaries, Homebrew tap, Scoop
  bucket (maintainer is on Windows — first-class).
- Quickstart must get a stranger to "it works" in under 30 seconds.
- LICENSE: MIT. Light CONTRIBUTING.md.
- Launch posts (maintainer, after v0.1.0): r/commandline, r/golang,
  Terminal Trove, Hacker News Show HN.

## 11. Milestones

1. **Skeleton** — listener + live-updating list pane; core/UI split in place.
2. **Inspector** — detail tabs, JSON highlighting, resize-safe responsive layout.
3. **Power** — replay, $EDITOR edit + diff, cURL copy, search/filter.
4. **Ship** — persistence, help overlay, tests + CI, README + VHS GIF,
   goreleaser, tag v0.1.0.

Each milestone ends with the app runnable and `go test ./...` green.

## 12. Open decisions (maintainer)

- ~~Final name~~ — **decided 2026-10-24: Hookline.** Folder, Go module and
  binary all use `hookline`. Still to check before public release: GitHub
  org/repo availability and the module path rename below.
- Default port `4040` — in use; no collisions observed.
- Go module path is locally `hookline`; rename to
  `github.com/<user>/hookline` once, immediately before publishing.

---

## Glossary (for the maintainer, not the implementer)

- **Webhook** — another service's server sends *you* an HTTP request when
  something happens (e.g. Stripe POSTs "payment succeeded" to your endpoint).
- **Replay** — re-send a previously captured request to your dev server to
  reproduce what happened, without waiting for the provider to send another.
- **Tunnel** — a bridge that gives your `localhost` listener a public URL so
  real providers (Stripe, GitHub) can reach it.
- **JSONL** — JSON Lines: one JSON object per line in a plain text file;
  greppable and append-only, ideal for a capture log.
- **OSC52** — a terminal escape sequence that copies text to the clipboard;
  it's why copy works even inside an SSH session.
