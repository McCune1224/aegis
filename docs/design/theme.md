# The constellation theme

The web UI is one Solid app over a PixiJS canvas, and both halves share a
palette, an elevation ramp, and a type scale. This records those tokens, the
four rendering defects they were hiding, and why every number is what it is.
It is the design record for the theme itself, the way `graph-view.md` is the
record for the camera.

## The decision

`web/src/app.css` holds one token set and no screen hard-codes a colour, a size,
or a radius. Everything below is one of those tokens.

### Elevation is a surface, not a shadow

A shadow on a dark ground is invisible, so it cannot be what tells an operator
that a panel sits above the page. The ramp is four surfaces:

| Token | Value | What sits on it |
| --- | --- | --- |
| `--void` | `#04060b` | the sky, and the well behind an input |
| `--sky` | `#080b13` | the app ground |
| `--surface-1` | `#0e121d` | a panel |
| `--surface-2` | `#141a28` | a panel header, the sidebar, the top bar, a table header |
| `--surface-3` | `#1b2233` | an overlay: the graph panel, a floating control |
| `--surface-4` | `#232c42` | an active nav row, a hovered control |

The previous sheet had three surfaces inside 4% of each other. The sidebar, the
top bar, and the panels all read as one plane, which is what "flat" meant.

Depth is finished with a one-pixel rim of light along the top edge
(`inset 0 1px 0 rgba(255,255,255,0.04)`), which is the cue that survives on a
dark background, plus a real shadow only on things that genuinely float.

### Borders are untinted

`--line` is `rgba(255,255,255,0.07)`, not a tinted blue. A tinted border on
every edge competes with the accent for the same job, and the accent then has
nowhere to go. The accent gets borders to itself through `--accent-line`.

### All three ink tones clear 4.5:1

| Token | Value | On `--surface-1` |
| --- | --- | --- |
| `--star` | `#eef2fa` | 16.6:1 |
| `--star-dim` | `#aab4c9` | 8.3:1 |
| `--star-faint` | `#8b96b5` | 6.4:1 |

The quietest tone previously measured 3.3:1 and was the one under the smallest
type: table column headers at 10.9px, the brand's subtitle at 9.9px, the sidebar
footer, and the graph hint. Contrast cannot be spent where legibility is already
thinnest, so the floor is 4.5:1 for every text tone rather than only for body
copy.

### Type: a 16px base on a 1.2 scale, and looser leading

`--text-md` is 1rem and the scale runs 0.6875 / 0.75 / 0.8125 / 0.875 / 1 /
1.125 / 1.375 / 1.75 rem. Nine of the previous sizes were below 13px.

Two adjustments come from how light text behaves on a dark ground. Halation —
irradiation — makes a glyph read about one weight heavier than it measures, so
nothing in the sheet is lighter than 400 and every label is 500 or 600. The same
effect crowds lines together, so body leading is 1.6 rather than the 1.45 the
sheet carried.

### One accent, used once per screen

`--accent` is `#7dd3fc`. The active nav row is a raised surface with a 2px rail
instead of a tinted row with glowing text, the blocked stat is a red rail
instead of a red glow, and `text-shadow` no longer appears on any chrome.
Glow belongs to the live flow, where it carries information.

### A bare `<button>` is still a button

Twenty-eight buttons across the CRUD screens carried no class and fell through
to the user agent's own chrome, which is what made those screens look
unfinished. `button` now has a real base: surface, border, radius, padding,
font, weight, and a focus ring. Four variants — `.btn`, `.btn-solid`,
`.btn-ghost`, `.btn-danger` — override colour and weight only, and hover and
focus always increase contrast rather than softening it.

### A form is a grid

A field is as wide as the value that goes in it. `form` is a grid of
`minmax(min(15rem,100%), 1fr)` columns, with a textarea, a fieldset, an action
row, or a tag list taking the whole row. The old single column stretched a name
field across 60rem.

## The starfield is generated, not hand-placed

The sky was twelve radial gradients with 1px radii. A radial gradient is
rasterised at the display's scale, so on a 2x panel each "star" was a smear.
`web/src/sky.ts` paints a tile of circles into an SVG data URI from a seeded
mulberry32, so a star stays a circle at any density and a reload cannot
reshuffle the sky. Two layers — a dense dim far field at 460px and a sparse
bright near one at 900px — give the sky a plane. Every circle is inset by its
own radius so the repeat has no seam.

## The glyph layer renders at the display's density

The Pixi application was initialised without a `resolution`, and Pixi defaults
to 1. The canvas was then stretched over its CSS box, so on a 2x display every
star, every label, and every flow particle was drawn at half density and
upscaled. `resolution: min(devicePixelRatio, 2)` with `autoDensity: true` fixes
it; the clamp exists because the fourth pixel of a 3x panel is not worth the
fill rate. Pixi's text renderer follows the renderer's resolution when a text's
own resolution is null, which is the default, so the labels become crisp with
the same change.

The sprite was 160px, drawn at up to 150 world px, and then multiplied by the
camera scale, which reaches 3. It is now 512px, its atmosphere falls to nothing
before the tile edge, its flare is a tapered ray rather than a two-pixel stroke,
and it has a hot core with a specular point. A star reads as a star instead of a
smudge.

## A label owns its layout box

The layered layout placed a node by a fixed 120x44 box while the label started
16px right of the centre and ran as far as the text needed, so two long domains
in one layer printed over each other. A cell is now the whole thing the operator
sees — the star, the gap, and the wider of the label and its detail line —
measured with an offscreen canvas, capped, and cut with an ellipsis past the
cap. `web/src/label.ts` holds that arithmetic and `web/src/label.test.ts` pins
it. A fit frames the cells, so no label is cropped.

## An edge is structure, not identity

Edges were stroked in the client tint. Two crossing edges then produced a patch
of client-coloured pixels with no client under it. They are a desaturated slate
now, which also keeps the tint vocabulary — sky is a client, mint a profile,
violet an upstream, amber a rule — reserved for nodes.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| measured token set in `app.css` | four surfaces, three inks, one accent, a 1.2 scale | chosen |
| carry a light theme too | a second token set and a toggle | rejected, the app is a dark instrument and the operator asked for the constellation, not a theme switch |
| a webfont (Inter or Geist) | one more asset in the bundle | rejected for now; the system stack already resolves to a humanist sans on every target, and the embed would have to carry the font |
| a canvas `BlurFilter` for the star glow | a real blur pass per node | rejected, a baked sprite costs nothing per frame and the glow never changes |
| mipmaps on the sprite | a larger texture pyramid | rejected, the sprite's finest feature is already soft, so minification has nothing to alias |
| hide uPlot's legend in CSS | one line of CSS | rejected, uPlot reserves the row at layout time, so hiding it left the height behind as dead space; the option turns it off |
| a hand-placed star list | keep the twelve gradients | rejected, they cannot survive a retina panel |
| the reveal claim cleared by any draw | simpler branch | rejected, a draw that races the save consumed the claim and dropped the selection |

## Where the numbers come from

- The 12-step scale and the role of each step, which the surface and border
  ramps follow: <https://www.radix-ui.com/colors/docs/palette-composition/understanding-the-scale>
- Elevation as surface in a dark theme, and the sunken/raised distinction:
  <https://atlassian.design/foundations/elevation>
- Halation, the weight bump, and the 10-20% leading increase:
  <https://uxdictionary.io/article/mitigating-text-halation-in-dark-mode-typography>
  and <https://typographyhandbook.com>
- Rendering options, and that `resolution` defaults to 1:
  <https://pixijs.download/dev/docs/app.ApplicationOptions.html>

## Verification

The Go suite, `go vet`, and `golangci-lint` are unchanged and green; the change
is web-only. `web/src/{sky,label}.test.ts` are new and pin the two pieces of
pure arithmetic this introduces.

Everything else is a browser check over CDP against the built binary, which is
the only surface a theme has. The probes live outside the repository; they drive
the real bundle and read the canvas back.

| Step | Signal | Result |
| --- | --- | --- |
| pan by (180, 90) | the centroid of the profile stars | moved by (180.1, 90.0) |
| zoom two steps on a star | the gap to its neighbour | 467.0px to 958.6px, ×2.053 |
| zoom two steps on a star | the star under the cursor | held to 0.6px |
| fit after a zoom | the gap | back to 467.0px |
| resize after a pan | the node under a fixed screen point | stayed within 0.15px |
| click a star | the details panel | opened with the node's name |
| create a profile | the panel and the revealed node | opened on `work` |
| drag unidentified onto a profile | `GET /default-profile` | the default moved |
| drag a profile onto another | the stored `extends` | set |
| drag a profile onto its own child | the server | refused, and the graph names it |

Two things about that table are worth keeping. The zoom check used to count lit
pixels, and that number now falls as a magnified star saturates to a white core;
the gap between two stars is the invariant that actually holds. And the check
that identifies the unidentified client star used to take the densest
client-tinted cluster, which only ever worked because crossing edges were
painted in the client tint. It now names each client star by clicking it and
takes the one that opens no panel, which is true by construction.

## What is not built

**No light theme.** The app is a dark instrument, and the tokens are declared on
`:root` rather than behind a media query.

**No measured contrast in CI.** The ratios in this document were computed by
hand. Nothing stops a later change from putting a 3:1 grey under 11px type
again; a test over the token table would.

**No per-user density.** The scale is fixed at a 16px base. An operator who
wants more rows per screen has the browser's own zoom, which scales the Pixi
canvas with everything else.
