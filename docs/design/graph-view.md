# The graph camera

The constellation is a PixiJS canvas, so a viewport is not CSS: panning, zooming,
and fitting are arithmetic on a camera, and the camera is the only view state the
graph keeps. This records that arithmetic and where it lives. It is the design
record for #58.

## The decision

The camera is `{x, y, scale}` in `web/src/camera.ts`, apart from the renderer.
Five functions move it, and each is a pure function of the camera and a screen
point (or, for `ensureVisible`, a world point):

- `toWorld` undoes the transform, so hit-testing and the camera cannot disagree
  about where a star is.
- `zoomAt` scales about a screen point, so whatever is under the pointer stays
  under it. Zooming about the corner instead makes one wheel step feel like a
  jump.
- `pan` moves by a screen delta.
- `frame` fits a world box into a viewport, centring it and leaving a margin for
  the labels that hang off a star.
- `ensureVisible` pans the least distance that brings one world point inside the
  viewport margins, which is how the node a create just placed comes into view
  without the whole sky being re-framed (`docs/design/graph-editing.md`).

The renderer owns no arithmetic beyond calling these and applying the result to
the world container. That is what makes pan, zoom, and fit testable without a
canvas: the tests are literal numbers, and the browser test checks that the
pixels followed.

## The camera survives a redraw

`fitToView` frames the constellation once, when the graph is first placed. Every
later redraw keeps the camera, because a redraw is usually a data change the
operator did not ask to see from a new angle. Adding a client used to re-centre
the whole sky, which throws away the star they were looking at.

The fit control asks for the framed view back, and it is the only other caller.
A resize does not re-frame either: the stars stay where they were and the
viewport shows more or less of the sky.

## Live flow

A decision arriving on the stream draws a particle along the edges its verdict took:
client to profile, then profile to upstream for an allowed query, and client to
profile alone for a blocked one, with a pulse left at the profile. The particle is
colored by verdict, so a block reads differently from an allow. The identity comes
from the stream rather than from the address, because a device identified by its
hardware address or by a lease cannot be found from the address alone.

`web/src/flow.ts` owns the animation state and the two numbers that bound it. A
particle is admitted at most once every `SPAWN_INTERVAL_MS`, so a busy network shows a
sample of its traffic rather than every query. At `MAX_PARTICLES` in flight the oldest
is dropped, so the newest is the one on screen. At the shipped interval the throttle
is what bounds concurrency, since a particle lives about a second on a short path, and
the cap is the backstop for a long one. `admit` gates the whole decision, so a blocked
query's pulse is refused with its streak rather than flashing alone.

## Verification

The camera has unit tests, and the graph has a browser check over CDP against
the built binary, which reads the canvas back and reports where the lit pixels
are:

| Step | Signal | Result |
| --- | --- | --- |
| pan by (180, 90) | centroid of the star field | moved by (180, 90) |
| wheel zoom in | lit pixel count | grew from 33k to 86k |
| fit | centroid | returned to within 1px of the first frame |
| click a star | the details panel | opened with the node's name |
| resize after a pan | the node under a fixed screen point | still the same node |

The last row is the one that catches the defect: without the guard the same
screen point lands on nothing, because the view has been re-framed underneath it.

The flow has unit tests for its budget, and a second browser check for the edge a
decision travels. It reads the star tints to find the rows and then the pixels of the
color only a blocked decision draws, which no star uses.

| Step | Signal | Result |
| --- | --- | --- |
| find the rows | star tints | client 199, profile 421, upstream 651 |
| 24 blocked queries | blockColor pixels | spanned y 206 to 460 |

The span is the point. It starts at the client row and stops at the profile row, so
the particle ran the edge the verdict named, and it never reached the upstream row the
allowed path continues to.

## What is not built

**No keyboard navigation and no minimap.** The graph is pointer-driven. A
keyboard path would need a focus model over the stars, and a minimap needs a
second camera and a scaled render pass; neither is asked for yet.

**No measured frame budget for the flow.** A burst of identical queries starts its
particles together, so they overlap and a pixel count cannot tell twenty from two
hundred. Frame time under a software renderer separated a throttled build from an
unthrottled one by 146 frames to 135 over two seconds, which is noise. The budget is
unit-tested and its effect on frame time needs a real GPU to measure.

**No camera persistence.** The view resets when the tab is left, because the
graph is unmounted with its Pixi application. Storing it would mean lifting the
camera out of the renderer, which is a real change and not a needed one.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| camera module, renderer applies it | pure functions over `{x, y, scale}` | chosen |
| camera inside the renderer | arithmetic inline in the pointer handlers | rejected, it is untestable without a canvas and it is the part with edge cases |
| re-frame on every redraw | fit whenever the topology changes | rejected, it discards the operator's view |
| clamp by clamping the offsets | keep the camera inside the content bounds | rejected, it fights a deliberate pan and the fit control already exists |
