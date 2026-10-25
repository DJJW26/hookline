# ◆ hookline

A local-first webhook inspector that lives in your terminal. Run one
binary, point webhooks at it, and watch deliveries land in a fast,
keyboard-driven TUI — inspect pretty JSON, replay to your dev server, edit
payloads and resend, or copy any delivery as a cURL command. Nothing leaves
your machine unless you press replay.

```
◆ hookline                                        ⏺ listening · 127.0.0.1:4040
╭ deliveries ──────────────────── 1 ─╮ ╭ body · POST /hooks/github · 7B ────╮
│  23:17:58 POST  /e2e/hook  43B json │ │ {                                    │
│                                    │ │   "event": "payment.succeeded",     │
│                                    │ │   "amount": 4200                     │
╰────────────────────────────────────╯ ╰──────────────────────────────────────╯
 j k select · tab pane · r replay · e edit · c curl · / find     vdev · 1 deliveries
```

## Why

Debugging webhooks means answering "what exactly did they send me?" while
providers retry and payloads scroll past in server logs. Web-based tools
(webhook.site, RequestBin) make you paste payloads into a browser; SaaS CLIs
want an account. hookline is none of that: offline, no signup, one static
binary, and it blends into whatever terminal you already customized.

## Features

- **Capture everything** — any method, path, header, content type; JSON,
  urlencoded forms, or binary (hex view). Bursts are queued,
  backpressure-aware: drops are counted and shown, never silently lost.
- **Inspect fast** — syntax-highlighted JSON, aligned headers, raw bytes,
  auto-response; unread markers for new arrivals; live search (`/`, matches
  headers too) and method filter (`f`).
- **Replay & edit** — resend a delivery byte-identical to any target (`r`),
  round-trip the body through `$EDITOR` with a diff review, then send (`e`).
  The target's answer lands in the response tab; toasts are colored by
  status class. Non-loopback targets ask for a confirming second `r`, and
  the target persists to `config.json` between runs.
- **Signature re-signing** — signature re-signing for Stripe, GitHub and
  Shopify so replays verify: start with `--sign stripe` and
  `HOOKLINE_SIGN_SECRET` and every resend (edited bodies included) carries
  a fresh signature. A missing secret refuses to send — never unsigned.
- **Export** — copy any delivery as a shell-safe cURL command (`c`) or the
  raw body (`y`). Copies use OSC 52, so they work inside SSH sessions.
- **Persistent** — history survives restarts as JSONL on disk
  (`--fresh` rotates it aside to `hookline-<timestamp>.jsonl`, keeping 5,
  instead of deleting).
- **Blend in** — transparent backgrounds, auto dark/light detection,
  truecolor with graceful degradation to 256/16/mono, `NO_COLOR` respected,
  window title set on start. Your terminal's theme shows through, not ours.

## Install

Requires Go 1.27+:

    go install github.com/DJJW26/hookline/cmd/hookline@latest

Or build from a checkout:

    git clone https://github.com/DJJW26/hookline.git && cd hookline
    go build -o hookline ./cmd/hookline

## Quickstart

    hookline                          # listens on http://127.0.0.1:4040
    curl -X POST http://127.0.0.1:4040/test -d '{"hello":"world"}'

For real providers (Stripe, GitHub, …), expose the listener with any tunnel
you like — `cloudflared tunnel --url http://127.0.0.1:4040`,
`ngrok http 4040` — and point the webhook at the public URL. A built-in
tunnel is on the roadmap.

## Keys

| Key | Action | Key | Action |
|---|---|---|---|
| `j k` / `↑ ↓` | select / scroll | `r` `R` | replay selected / latest |
| `PgUp/PgDn` | page (or ctrl+u/d) | `e` | edit body & resend |
| `n` | next unread | `c` / `y` | copy as cURL / copy body |
| `tab` | switch pane | `t` | set replay target |
| `enter` | inspect delivery | `f` | filter by method |
| `1-4` `h l` | detail tabs | `x` | clear history (confirm) |
| `/` | search (incl. headers) | `?` | help |
| `g` `G` | top / bottom | `q` | quit |

Mouse works too: click to select, wheel to scroll.

## Themes

`--theme auto` (default) follows your terminal's light/dark background.
Also available: `dark` (Dusk — purple accents on your terminal's own
canvas), `light` (Linen), `mono` (Graphite — zero color, weight-only
hierarchy). The palette lives in `internal/style/theme.go`; themes are
registry entries, so adding one is a single struct.

## Flags & data

    --port 4040        listen port            --status 200   reply status
    --bind 127.0.0.1   listen address         --target URL   default replay target
    --theme auto       dark|light|mono|auto   --fresh        rotate history aside
    --sign P           re-sign replays: stripe|github|shopify
    --version

History is JSONL in `%LOCALAPPDATA%\hookline` (Windows) or
`$XDG_DATA_HOME/hookline` (elsewhere); override with `HOOKLINE_DATA_DIR`.
The replay target lives in `config.json` next to it. With `--sign`, the
webhook secret comes from `HOOKLINE_SIGN_SECRET` (`whsec_…` Stripe secrets
are decoded automatically). Copies fall back to
`HOOKLINE_CLIP_CMD` (e.g. `clip.exe`, `wl-copy`) on terminals without
OSC 52.

## Status

v0.1.1 — working MVP, hardened per `docs/FINDINGS.md`. See `SPEC.md` for
the product spec and `docs/ARCHITECTURE.md` for how it works. Roadmap:
built-in Cloudflare quick tunnel, side-by-side delivery diffing, in-TUI
body editor, `--json` headless mode.

Releases are automated on tags (GoReleaser); the demo GIF is rendered by
`.vhs/demo.tape`.

MIT licensed. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
and [Lip Gloss](https://github.com/charmbracelet/lipgloss).
