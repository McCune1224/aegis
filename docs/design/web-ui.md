# The Workbench console

The web UI is a Swiss / international-style ops workbench built from the
backend's data model: a resolver with a live query stream, policy objects
(profiles, rules, schedules), identity (clients), and infrastructure
(upstreams, sources, access). One numbered text index, one utility line, one
canvas, one status line. Paper, black ink, a single red, hairline rules. This
records the architecture, the surface language, and the rendering rules. It is
the design record for the 2026-09 UI rebuild.

## The decision

The console is organised by operator job, not by API resource. Twelve flat
tabs became nine rail views; Upstreams, Sources, and Settings are sub-views of
one System hub. The rail is a numbered typographic index, not an icon strip.
Editing never happens inline under a list: every create and edit form opens in
a right-side drawer. Lists are sortable tables or a master-inspector pair
(Clients, Profiles). The Overview is a live workbench: giant numerals in a
ruled telemetry strip, the traffic chart, and a scrolling query feed. A
command palette (`Ctrl/Cmd+K`) jumps between views and runs the two global
actions; it closes on a real Escape key, as does the drawer.

## Surface language: Swiss

Paper `#f4f3ee`, ink `#141414`, two grays, and one accent red `#e32119`.
Functional markers only where meaning requires: green for allowed, amber for
caution, blue for neutral info. Verdicts are a colored marker square plus a
caps word, never a filled pill — color never carries the message alone. No
radius, no shadows, no gradients, no transitions: Swiss is instant. Type
carries hierarchy: 40px bold numerals for telemetry, uppercase tracked
micro-labels, and mono for every data value (domains, addresses, counts,
timestamps). Rhythm comes from rules: a 2px rule anchors each section, 1px
hairlines separate rows. The brand is the wordmark "AEGIS" closed by a red
period.

## What it refuses

- **Decoration.** No gradients, glows, blur, or illustration. The previous
  dark themes (industrial, clay) carried mood; Swiss carries structure. The
  starfield was removed with the dark themes. The Constellation tab is the one
  figurative surface, redrawn as a plain node graph: hairline edges, ink
  markers by shape (circle client, square profile, solid diamond upstream,
  red square rule), caps labels, a red square for selection, and live query
  pulses as small red and green squares travelling the edges. The Pixi WebGL
  sprite renderer and its dependency were deleted with the glow.
- **Webfonts.** The console must render on a LAN with no internet (the Pi
  deployment). Helvetica-class system sans for chrome, system mono for data.
- **Dark mode.** Swiss type contrast is designed for paper. `color-scheme:
  light` only.
- **Inline edit forms under lists.** The drawer is the one editing surface.

## Layout

Three breakpoints, no JavaScript:

- `>940px`: workbench layouts at full width (Overview splits into chart column
  and feed column; Clients and Profiles split into list and inspector).
- `760–940px`: the Overview stacks into one continuous scroll; the rail drops
  its wordmark and labels, becoming a numbered column.
- `<760px`: workbenches stack (list capped at 40% height), drawers go full
  width, the utility line drops the view name and palette label.

The grid uses `minmax(0, 1fr)` for the canvas column; `1fr` alone sizes
tracks to min-content and the utility line cannot shrink, which overflowed at
375px. Flex children inside scroll containers carry `flex-shrink: 0`, or they
squash instead of scrolling (the Overview on mobile taught this twice).

## The table rule

Row separators are `box-shadow: inset 0 -1px 0` on the `tr`, never
`border-bottom` on the `td`. Per-cell hairlines smear into vertical seams in
some rasterizers (reproduced in headless Chromium at 1x and 2x, collapse and
separate modes alike); a row-level shadow paints one continuous line. Swiss
makes the header rule heavy (`inset 0 -2px 0 var(--ink)`) and the last body
rule closes the table in ink.

## Verification

The harness in `docs/testing.md` applies: pure modules carry exact-value
tests, and the rendered surface is judged by screenshots. The rebuild was
verified against the real binary (temp SQLite, embedded bundle) driven over
raw CDP at 375, 768, and 1280 CSS px: all nine rail views plus the three
System sub-views, asserting no horizontal overflow, the paper background, the
status pill and strip, a clean console, the palette opening and closing on a
real Escape key event, and drawer presence on form views. Seeded data flows
through the real API and real DNS (block, rewrite, and allow verdicts all
visible). The CDP driver is dependency-free (Node's WebSocket); a stale
page's SSE reconnect noise during server restarts is filtered, not silenced
elsewhere. Synthetic (untrusted) keyboard events do not trigger Solid's
delegated handlers; the harness sends real keys through `Input.dispatchKeyEvent`.
