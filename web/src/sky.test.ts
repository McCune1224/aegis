import { describe, expect, it } from "vitest";
import { starField, starLayer } from "./sky";

// The layer is a CSS url() around a percent-encoded SVG, which is how it ends
// up in a background-image without a network request.
function markup(layer: string): string {
  const match = /^url\("data:image\/svg\+xml,([\s\S]*)"\)$/.exec(layer);
  if (!match) {
    throw new Error(`not a star layer: ${layer.slice(0, 40)}`);
  }
  return decodeURIComponent(match[1]);
}

const tile = { seed: 7, count: 12, size: 400, minRadius: 0.4, maxRadius: 1.2, opacity: 0.8 };

describe("starLayer", () => {
  it("draws the number of stars it was asked for", () => {
    expect(markup(starLayer(tile)).match(/<circle/g)).toHaveLength(12);
    expect(markup(starLayer({ ...tile, count: 40 })).match(/<circle/g)).toHaveLength(40);
  });

  it("keeps every star a radius inside the tile so the repeat has no seam", () => {
    const centers = [...markup(starLayer(tile)).matchAll(/cx="([\d.]+)" cy="([\d.]+)"/g)];
    expect(centers).toHaveLength(12);
    for (const [, cx, cy] of centers) {
      expect(Number(cx)).toBeGreaterThanOrEqual(1.2);
      expect(Number(cx)).toBeLessThanOrEqual(400 - 1.2);
      expect(Number(cy)).toBeGreaterThanOrEqual(1.2);
      expect(Number(cy)).toBeLessThanOrEqual(400 - 1.2);
    }
  });

  it("is stable for a seed, so a repaint cannot reshuffle the sky", () => {
    expect(starLayer(tile)).toBe(starLayer(tile));
    expect(starLayer(tile)).not.toBe(starLayer({ ...tile, seed: 8 }));
  });

  it("stays inside the radius it was given", () => {
    const radii = [...markup(starLayer(tile)).matchAll(/r="([\d.]+)"/g)].map(([, r]) => Number(r));
    for (const radius of radii) {
      expect(radius).toBeGreaterThanOrEqual(0.4);
      expect(radius).toBeLessThanOrEqual(1.2);
    }
  });
});

describe("starField", () => {
  it("lays a dense far field behind a sparse near one", () => {
    const field = starField();
    expect(markup(field.far).match(/<circle/g)!.length).toBeGreaterThan(
      markup(field.near).match(/<circle/g)!.length,
    );
    expect(markup(field.far)).not.toBe(markup(field.near));
  });
});
