// The live flow is the animation the graph draws for the queries arriving on the
// stream. It owns how many particles are in flight and how often a new one starts,
// so a busy network cannot fill the canvas with them, and it is a plain value so
// the budget can be tested without a canvas.

export type Point = { x: number; y: number };

export type Segment = { from: Point; to: Point };

export type Particle = {
  segments: Segment[];
  travelled: number;
  length: number;
  speed: number;
  color: number;
};

export type Pulse = { x: number; y: number; age: number; color: number };

export type Flow = {
  particles: Particle[];
  pulses: Pulse[];
  // lastSpawn is when the flow last admitted a particle, in milliseconds from the
  // clock the caller reads.
  lastSpawn: number;
};

// MAX_PARTICLES bounds how many streaks are drawn at once. Each one is a line and
// a dot on the effects layer, so the bound is what keeps a thousand queries a
// second from turning the canvas into a wash.
export const MAX_PARTICLES = 120;

// SPAWN_INTERVAL_MS is the shortest gap between two admitted particles. A busy
// network then shows a sample of its traffic rather than every query.
export const SPAWN_INTERVAL_MS = 50;

// PULSE_LIFE_SECONDS is how long the flash at the node a query reached lasts.
export const PULSE_LIFE_SECONDS = 0.5;

// TRAIL_LENGTH is how far behind the head of a particle its trail is drawn, and
// OVERSHOOT is how far past the end of its path a particle runs before it is
// dropped, so the head reaches the node before it goes.
const TRAIL_LENGTH = 26;
const OVERSHOOT = 24;

export function emptyFlow(): Flow {
  return { particles: [], pulses: [], lastSpawn: Number.NEGATIVE_INFINITY };
}

// admit reports whether one decision may start a particle. A flow with nothing in
// flight always admits one, so a single query on a quiet network is never dropped,
// and one at its cap makes room by dropping the oldest rather than refusing the
// newest.
export function admit(flow: Flow, now: number): boolean {
  const quiet = flow.particles.length === 0;
  if (!quiet && now - flow.lastSpawn < SPAWN_INTERVAL_MS) {
    return false;
  }
  if (flow.particles.length >= MAX_PARTICLES) {
    flow.particles.shift();
  }
  flow.lastSpawn = now;
  return true;
}

export function segment(from: Point, to: Point): Segment {
  return { from: { x: from.x, y: from.y }, to: { x: to.x, y: to.y } };
}

export function distance(one: Segment): number {
  return Math.hypot(one.to.x - one.from.x, one.to.y - one.from.y);
}

export function streak(segments: Segment[], color: number, speed: number): Particle {
  return {
    segments,
    travelled: 0,
    length: segments.reduce((total, current) => total + distance(current), 0),
    speed,
    color,
  };
}

// pointAt is the point one particle's path has reached after travelling this far,
// clamped to the last segment's end.
export function pointAt(segments: Segment[], travelled: number): Point {
  let remaining = travelled;
  for (const one of segments) {
    const length = distance(one);
    if (remaining <= length) {
      const ratio = length === 0 ? 0 : remaining / length;
      return {
        x: one.from.x + (one.to.x - one.from.x) * ratio,
        y: one.from.y + (one.to.y - one.from.y) * ratio,
      };
    }
    remaining -= length;
  }
  return segments[segments.length - 1]?.to ?? { x: 0, y: 0 };
}

// trail is the two ends of the streak one particle draws this frame.
export function trail(particle: Particle): { from: Point; to: Point } {
  return {
    from: pointAt(particle.segments, Math.max(0, particle.travelled - TRAIL_LENGTH)),
    to: pointAt(particle.segments, Math.min(particle.travelled, particle.length)),
  };
}

// advance moves every particle along its path, drops the ones that have run past
// their end, and ages the pulses. It returns whether anything is left to draw.
export function advance(flow: Flow, deltaSeconds: number): boolean {
  for (const particle of flow.particles) {
    particle.travelled += particle.speed * deltaSeconds;
  }
  for (let index = flow.particles.length - 1; index >= 0; index--) {
    const particle = flow.particles[index];
    if (particle.travelled > particle.length + OVERSHOOT) {
      flow.particles.splice(index, 1);
    }
  }
  for (let index = flow.pulses.length - 1; index >= 0; index--) {
    const pulse = flow.pulses[index];
    pulse.age += deltaSeconds;
    if (pulse.age > PULSE_LIFE_SECONDS) {
      flow.pulses.splice(index, 1);
    }
  }
  return flow.particles.length > 0 || flow.pulses.length > 0;
}
