import { describe, expect, it } from "vitest";
import {
  advance,
  admit,
  emptyFlow,
  MAX_PARTICLES,
  PULSE_LIFE_SECONDS,
  segment,
  streak,
  SPAWN_INTERVAL_MS,
  trail,
} from "./flow";

function flowWith(count: number) {
  const flow = emptyFlow();
  for (let index = 0; index < count; index++) {
    flow.particles.push(streak([segment({ x: 0, y: 0 }, { x: 100, y: 0 })], 1, 100));
  }
  return flow;
}

describe("admit", () => {
  it("admits a particle on a flow with nothing in flight", () => {
    const flow = emptyFlow();

    expect(admit(flow, 1000)).toBe(true);
    expect(flow.particles.length).toBe(0);
  });

  it("refuses a second particle inside the interval", () => {
    const flow = flowWith(1);
    flow.lastSpawn = 1000;

    expect(admit(flow, 1000 + SPAWN_INTERVAL_MS - 1)).toBe(false);
  });

  it("admits a second particle once the interval has passed", () => {
    const flow = flowWith(1);
    flow.lastSpawn = 1000;

    expect(admit(flow, 1000 + SPAWN_INTERVAL_MS)).toBe(true);
  });

  it("admits a particle on a quiet flow even inside the interval", () => {
    const flow = emptyFlow();
    flow.lastSpawn = 1000;

    expect(admit(flow, 1001)).toBe(true);
  });

  it("makes room by dropping the oldest particle at the cap", () => {
    const flow = flowWith(MAX_PARTICLES);
    flow.particles[0].color = 111;
    flow.lastSpawn = 0;

    expect(admit(flow, SPAWN_INTERVAL_MS)).toBe(true);
    expect(flow.particles.length).toBe(MAX_PARTICLES - 1);
    expect(flow.particles[0].color).not.toBe(111);
  });
});

describe("advance", () => {
  it("moves a particle along its path", () => {
    const flow = flowWith(1);

    advance(flow, 0.5);

    expect(flow.particles[0].travelled).toBe(50);
  });

  it("drops a particle that has run past the end of its path", () => {
    const flow = flowWith(1);
    flow.particles[0].travelled = 100;

    advance(flow, 1);

    expect(flow.particles).toHaveLength(0);
  });

  it("ages a pulse out after its life", () => {
    const flow = emptyFlow();
    flow.pulses.push({ x: 0, y: 0, age: 0, color: 1 });

    advance(flow, PULSE_LIFE_SECONDS + 0.1);

    expect(flow.pulses).toHaveLength(0);
  });

  it("reports whether anything is left to draw", () => {
    expect(advance(flowWith(1), 0.1)).toBe(true);
    expect(advance(emptyFlow(), 0.1)).toBe(false);
  });
});

describe("trail", () => {
  it("draws behind the head of a particle that has just started", () => {
    const flow = flowWith(1);

    const one = trail(flow.particles[0]);

    expect(one.from).toEqual({ x: 0, y: 0 });
    expect(one.to).toEqual({ x: 0, y: 0 });
  });

  it("draws the trail behind a head that has reached the end", () => {
    const flow = flowWith(1);
    flow.particles[0].travelled = 100;

    const one = trail(flow.particles[0]);

    expect(one.to).toEqual({ x: 100, y: 0 });
    expect(one.from).toEqual({ x: 74, y: 0 });
  });

  it("clamps both ends to the end of the path", () => {
    const flow = flowWith(1);
    flow.particles[0].travelled = 200;

    const one = trail(flow.particles[0]);

    expect(one.to).toEqual({ x: 100, y: 0 });
    expect(one.from).toEqual({ x: 100, y: 0 });
  });
});
