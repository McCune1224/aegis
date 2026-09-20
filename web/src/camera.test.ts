import { describe, expect, it } from "vitest";
import { frame, pan, toWorld, zoomAt, type Camera } from "./camera";

const identity: Camera = { x: 0, y: 0, scale: 1 };

describe("zoomAt", () => {
  it("keeps the world point under the cursor where it is", () => {
    const before = { x: 100, y: 50, scale: 1.5 };
    const anchor = { x: 240, y: 120 };
    const world = toWorld(before, anchor.x, anchor.y);

    const after = zoomAt(before, 1.8, anchor.x, anchor.y);
    const moved = toWorld(after, anchor.x, anchor.y);

    expect(after.scale).toBeCloseTo(2.7, 6);
    expect(moved.x).toBeCloseTo(world.x, 6);
    expect(moved.y).toBeCloseTo(world.y, 6);
  });

  it("stops at the scale limits", () => {
    expect(zoomAt(identity, 100, 0, 0).scale).toBe(3);
    expect(zoomAt(identity, 0.0001, 0, 0).scale).toBe(0.4);
  });

  it("does not move the camera when the factor changes nothing", () => {
    expect(zoomAt(identity, 1, 400, 300)).toEqual(identity);
  });
});

describe("pan", () => {
  it("moves the camera by the screen delta", () => {
    expect(pan({ x: 10, y: 20, scale: 2 }, -5, 7)).toEqual({ x: 5, y: 27, scale: 2 });
  });
});

describe("toWorld", () => {
  it("undoes the camera transform", () => {
    const camera = { x: 40, y: -10, scale: 0.5 };
    expect(toWorld(camera, 40, -10)).toEqual({ x: 0, y: 0 });
    expect(toWorld(camera, 140, 90)).toEqual({ x: 200, y: 200 });
  });
});

describe("frame", () => {
  const box = { minX: 0, minY: 0, maxX: 400, maxY: 200 };

  it("centres the box in the viewport", () => {
    const camera = frame(box, 400, 400, 0);
    // The box is wider than it is tall, so the width decides the scale.
    expect(camera.scale).toBe(1);
    expect(camera.x).toBe(0);
    expect(camera.y).toBe(100);
  });

  it("leaves room for the labels around the stars", () => {
    const camera = frame(box, 400, 400, 220);
    expect(camera.scale).toBeCloseTo(400 / 620, 6);
  });

  it("never zooms past the limit for a tiny graph", () => {
    const camera = frame({ minX: 0, minY: 0, maxX: 1, maxY: 1 }, 1000, 1000, 0);
    expect(camera.scale).toBe(1.4);
  });

  it("puts the middle of the box in the middle of the viewport", () => {
    const camera = frame(box, 800, 600, 40);
    const middle = toWorld(camera, 400, 300);
    expect(middle.x).toBeCloseTo(200, 6);
    expect(middle.y).toBeCloseTo(100, 6);
  });
});
