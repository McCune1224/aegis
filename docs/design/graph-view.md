# The graph camera

The constellation is a PixiJS canvas, so a viewport is not CSS: panning, zooming,
and fitting are arithmetic on a camera, and the camera is the only view state the
graph keeps. This records that arithmetic and where it lives. It is the design
record for #58.

## The decision

The camera is `{x, y, scale}` in `web/src/camera.ts`, apart from the renderer.
Four functions move it, and each is a pure function of the camera and a screen
point:

- `toWorld` undoes the transform, so hit-testing and the camera cannot disagree
  about where a star is.
- `zoomAt` scales about a screen point, so whatever is under the pointer stays
  under it. Zooming about the corner instead makes one wheel step feel like a
  jump.
- `pan` moves by a screen delta.
- `frame` fits a world box into a viewport, centring it and leaving a margin for
  the labels that hang off a star.

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

## What is not built

**No keyboard navigation and no minimap.** The graph is pointer-driven. A
keyboard path would need a focus model over the stars, and a minimap needs a
second camera and a scaled render pass; neither is asked for yet.

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
