# The Workbench console

The web UI is a claymorphic ops workbench built from the backend's data model:
a resolver with a live query stream, policy objects (profiles, rules,
schedules), identity (clients), and infrastructure (upstreams, sources,
access). One icon rail, one command bar, one canvas, one status strip. This
records the architecture, the surface language, and the rendering rules. It is
the design record for the 2026-09 UI rebuild.

## The decision

The console is organised by operator job, not by API resource. Twelve flat
tabs became nine rail views; Upstreams, Sources, and Settings are sub-views of
one System hub. Editing never happens inline under a list: every create and
edit form opens in a right-side drawer. Lists are sortable tables or a
master-inspector pair (Clients, Profiles). The Overview is a live workbench:
an inline telemetry strip, the traffic chart, and a scrolling query feed, with
no stat cards.

A command palette (`Ctrl/Cmd+K`) jumps to any view and runs the two global
actions (toggle the live stream, refresh the services catalog). Arrow keys and
Enter drive it; Escape closes it; the drawer closes on Escape and on scrim
click and takes focus when it opens.

## Surface language: dark clay

Claymorphism, adapted to a dark console. The dataset recipe is rounded 16-24px
surfaces, thick borders, and double shadows; the pastels come from the
constellation graph, which keeps one color code across the app:

| Token | Value | Meaning in the chrome | Meaning in the graph |
| --- | --- | --- | --- |
| `--accent` | `#7dd3fc` | interactive accent, links, focus | a client star |
| `--allow` | `#6ee7b7` | allowed verdicts, saved, profile badges | a profile star |
| `--block` | `#fb7185` | blocked verdicts, destructive actions | a blocked query's flow |
| `--warn` | `#fcd34d` | caution badges | a rule star |
| `--info` | `#c4b5fd` | neutral info badges | an upstream star |

Clay shadows are layered: an outer drop, an inner top highlight, and an inner
bottom shade (`--clay`); inputs and search invert into pressed wells
(`--clay-inset`); switch knobs carry their own small clay. Presses translate
1px with a soft overshoot easing. The starfield behind everything stays
seeded, static, and `aria-hidden`; the Constellation tab is the only literal
star rendering.

## What it refuses

- **Webfonts.** The console must render on a LAN with no internet (the Pi
  deployment). System sans for chrome, system mono for data.
- **Light mode.** Clay reads as volume against dark surfaces; a daylight twin
  would be a second design. `color-scheme: dark` only.
- **Inline edit forms under lists.** They pushed every list off the fold and
  made the create path invisible. The drawer is the one editing surface.

## Layout

Three breakpoints, no JavaScript:

- `>940px`: workbench layouts at full width (Overview splits into chart column
  and feed column; Clients and Profiles split into list and inspector).
- `760–940px`: the Overview stacks into one continuous scroll.
- `<760px`: workbenches stack (list on top, capped at 40% height), drawers go
  full width, the command bar drops the view name and the palette label.

The grid uses `minmax(0, 1fr)` for the canvas column; `1fr` alone sizes
tracks to min-content and the command bar cannot shrink below its search
pill, which overflowed at 375px.

## The table rule

Row separators are `box-shadow: inset 0 -1px 0 var(--line)` on the `tr`, never
`border-bottom` on the `td`. Per-cell hairlines smear into vertical seams in
some rasterizers (reproduced in headless Chromium at 1x and 2x, collapse and
separate modes alike); a row-level shadow paints one continuous line. The
same rule puts the header underline on the `thead` row.

## Verification

The harness in `docs/testing.md` applies: pure modules carry exact-value
tests, and the rendered surface is judged by screenshots. The rebuild was
verified against the real binary (temp SQLite, embedded bundle) driven over
raw CDP at 375, 768, and 1280 CSS px: all nine rail views plus the three
System sub-views, asserting no horizontal overflow, a painted sky, the status
pill and strip, a clean console, the palette opening and closing on a real
Escape key event, and drawer presence on form views. Seeded data flows
through the real API and real DNS (block, rewrite, and allow verdicts all
visible). The CDP driver is dependency-free (Node's WebSocket); a stale
page's SSE reconnect noise during server restarts is filtered, not silenced
elsewhere. Synthetic (untrusted) keyboard events do not trigger Solid's
delegated handlers; the harness sends real keys through `Input.dispatchKeyEvent`.
