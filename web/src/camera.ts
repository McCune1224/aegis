// The camera is the one piece of view state that is pure arithmetic, so it
// lives here rather than in the renderer: pan, zoom, and fit are testable
// without a canvas, and the renderer only applies the result.

export type Camera = { x: number; y: number; scale: number };

// The zoom limits. A constellation is readable between these; past either end
// the labels are unreadable or the sky is a dot.
export const MIN_SCALE = 0.4;
export const MAX_SCALE = 3;

// The most a fit is allowed to magnify a small graph.
const FIT_CEILING = 1.4;

export type Box = { minX: number; minY: number; maxX: number; maxY: number };

const clamp = (scale: number) => Math.min(MAX_SCALE, Math.max(MIN_SCALE, scale));

// toWorld turns a screen point into the world point under it.
export function toWorld(camera: Camera, screenX: number, screenY: number): { x: number; y: number } {
  return { x: (screenX - camera.x) / camera.scale, y: (screenY - camera.y) / camera.scale };
}

// zoomAt scales about a screen point, so whatever is under the pointer stays
// under it. Zooming about the corner instead makes a wheel step feel like a
// jump.
export function zoomAt(camera: Camera, factor: number, screenX: number, screenY: number): Camera {
  const scale = clamp(camera.scale * factor);
  if (scale === camera.scale) {
    return camera;
  }
  const applied = scale / camera.scale;
  return {
    x: screenX - applied * (screenX - camera.x),
    y: screenY - applied * (screenY - camera.y),
    scale,
  };
}

// pan moves the camera by a screen delta.
export function pan(camera: Camera, dx: number, dy: number): Camera {
  return { x: camera.x + dx, y: camera.y + dy, scale: camera.scale };
}

// frame fits a world box into a viewport, centring it and leaving the given
// margin for the labels that hang off a star.
export function frame(box: Box, width: number, height: number, margin: number): Camera {
  const contentWidth = box.maxX - box.minX + margin;
  const contentHeight = box.maxY - box.minY + margin;
  if (contentWidth <= 0 || contentHeight <= 0 || width <= 0 || height <= 0) {
    return { x: 0, y: 0, scale: 1 };
  }

  const scale = Math.min(FIT_CEILING, width / contentWidth, height / contentHeight);
  return {
    x: (width - (box.minX + box.maxX) * scale) / 2,
    y: (height - (box.minY + box.maxY) * scale) / 2,
    scale,
  };
}
