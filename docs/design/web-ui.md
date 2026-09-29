# The Observatory console

The web UI is one dark theme whose palette is shared with the constellation
graph, panels floating on a seeded starfield. This records the system, what it
deliberately refuses, and the one rendering rule the tables must keep. It is
the design record for the 2026-09 UI rebuild.

## The decision

One color vocabulary for the whole app and the sky map. The graph's four star
tints are the app's semantic colors:

| Token | Value | Meaning in the chrome | Meaning in the graph |
| --- | --- | --- | --- |
| `--accent` | `#7dd3fc` | interactive accent, links, focus | a client star |
| `--allow` | `#6ee7b7` | allowed verdicts, saved, profile badges | a profile star |
| `--block` | `#fb7185` | blocked verdicts, destructive actions | a blocked query's flow |
| `--warn` | `#fcd34d` | caution badges | a rule star |
| `--info` | `#c4b5fd` | neutral info badges | an upstream star |

A verdict badge in the log and the star it would be on the map are the same
color, so an operator learns one code. The backgrounds are a deep blue-black
ramp (`--void` through `--surface-3`) and the starfield behind everything is
`sky.ts`'s seeded SVG tiles: two layers, dense-dim far and sparse-bright near,
stable across reloads, static, and `aria-hidden`. Flavor lives in the
background and the Constellation tab; no card, button, or icon paints a star.

## What it refuses

- **Webfonts.** The console must render on a LAN with no internet, which is
  the Pi deployment. System sans for chrome, system mono for data (domains,
  addresses, counts, timestamps).
- **Glassmorphism.** The generator's suggestion for this product class. Blur
  is limited to the sticky topbar and the graph chrome, where content actually
  scrolls underneath. Panels are opaque; data density beats depth effects.
- **Zero-radius industrial.** The previous theme's 0px corners made dense
  tables read as spreadsheets of grey. Controls are 8px, panels 10px, badges
  pill-shaped.
- **Light mode.** A starfield has no daylight twin. `color-scheme: dark` only.

## Layout

A grouped sidebar (Observe / Policy / Controls / Network / System), a sticky
topbar with the page title and the upstream status pill, and a content column
capped at 1360px. Twelve tabs, including the restored Constellation graph
(`Graph.tsx`), which the previous redesign had orphaned along with `sky.ts`.

Three breakpoints, no JavaScript:

- `>1080px`: full sidebar with labels.
- `720–1080px`: 64px icon rail, labels dropped, `title` attributes carry names.
- `<720px`: the nav becomes a horizontal scroll row of labeled chips; brand
  and footer hidden; stat cards fall to a 2×2 grid; rows stack.

## The table rule

Row separators are `box-shadow: inset 0 -1px 0 var(--line)` on the `tr`, never
`border-bottom` on the `td`. Per-cell hairlines smear into vertical seams in
some rasterizers (reproduced in headless Chromium at 1x and 2x, collapse and
separate modes alike); a row-level shadow paints one continuous line. The
same rule puts the header underline on the `thead` row. If a future table
style reintroduces per-cell borders, drive the screenshot harness before
trusting it.

## Verification

The harness in `docs/testing.md` applies: pure modules carry exact-value
tests, and the rendered surface is judged by screenshots. The rebuild was
verified against the real binary (temp SQLite, embedded bundle) driven over
raw CDP at 375, 768, and 1280 CSS px, all twelve tabs, asserting no horizontal
overflow, a painted sky, the status pill, and a clean console. The CDP driver
is dependency-free (Node's WebSocket); a stale page's SSE reconnect noise
during server restarts is filtered, not silenced elsewhere.

Forms are capped at a 640px measure, every control has a visible
`:focus-visible` ring, the active nav item carries `aria-current="page"`, and
no button is icon-only without a label or `title`. Reduced-motion preference
flattens transitions and dims the near starfield.
