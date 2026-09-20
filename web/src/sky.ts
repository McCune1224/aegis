// The starfield behind every screen. A tile of circles is painted into an SVG
// data URI rather than a handful of CSS radial gradients, because a 1px
// gradient is rasterised at the display's scale and turns into a smear on a
// hi-dpi panel, while an SVG stays a circle at any density.

export type StarTile = {
  seed: number;
  count: number;
  size: number;
  minRadius: number;
  maxRadius: number;
  opacity: number;
};

export type StarField = { far: string; near: string };

// mulberry32, so a seed always paints the same sky and a reload cannot
// reshuffle it under the operator.
function random(seed: number): () => number {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let value = state;
    value = Math.imul(value ^ (value >>> 15), value | 1);
    value ^= value + Math.imul(value ^ (value >>> 7), value | 61);
    return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
  };
}

const tints = ["#ffffff", "#eef2fa", "#d6e6ff", "#b3cdf2"];

export function starLayer(tile: StarTile): string {
  const next = random(tile.seed);
  const inset = tile.maxRadius;
  const span = Math.max(1, tile.size - inset * 2);
  const circles: string[] = [];

  for (let index = 0; index < tile.count; index++) {
    const x = inset + next() * span;
    const y = inset + next() * span;
    const radius = tile.minRadius + next() * (tile.maxRadius - tile.minRadius);
    const tint = tints[Math.min(tints.length - 1, Math.floor(next() * tints.length))];
    const alpha = tile.opacity * (0.4 + next() * 0.6);
    circles.push(
      `<circle cx="${x.toFixed(2)}" cy="${y.toFixed(2)}" r="${radius.toFixed(2)}" fill="${tint}" opacity="${alpha.toFixed(2)}"/>`,
    );
  }

  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${tile.size}" height="${tile.size}" viewBox="0 0 ${tile.size} ${tile.size}">${circles.join("")}</svg>`;
  return `url("data:image/svg+xml,${encodeURIComponent(svg)}")`;
}

// The far field is dense and dim, the near field sparse and bright, which is
// what gives the sky a plane to sit on instead of a single speckle.
export function starField(): StarField {
  return {
    far: starLayer({ seed: 20260920, count: 130, size: 460, minRadius: 0.35, maxRadius: 0.95, opacity: 0.42 }),
    near: starLayer({ seed: 611, count: 26, size: 900, minRadius: 0.7, maxRadius: 1.7, opacity: 0.75 }),
  };
}
