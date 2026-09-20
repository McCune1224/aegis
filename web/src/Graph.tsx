import ELK from "elkjs/lib/elk.bundled.js";
import { Application, Container, Graphics, Sprite, Text, Texture } from "pixi.js";
import { createEffect, createSignal, For, onCleanup, Show } from "solid-js";
import type { Client, Profile, ProfileInput, Rule } from "./api";
import { BLOCKING_MODES } from "./api";
import { frame, ensureVisible, pan, toWorld, zoomAt, type Box, type Camera } from "./camera";
import type { Decision } from "./api";
import { effectiveMode } from "./resolve";
import type { QueryLog } from "./querylog";
import { buildTopology, clientFor, upstreamNodeID, type NodeKind, type Topology } from "./topology";
import { admit, advance, emptyFlow, PULSE_LIFE_SECONDS, segment, streak, trail, type Flow } from "./flow";
import { cellWidth, fitLabel, LABEL_MAX, ROW_HEIGHT, STAR_CELL, type Measure } from "./label";
import { linkIntent, type LinkIntent } from "./edit";

type Props = {
  profiles: Profile[];
  clients: Client[];
  rules: Rule[];
  defaultProfile: string;
  upstreams: string[];
  log: QueryLog;
  onSaveClient: (name: string, input: { profile: string; notes: string; addresses: string[]; macs: string[]; prefixes: string[] }) => Promise<void>;
  onSaveProfile: (name: string, input: ProfileInput) => Promise<void>;
  onSetDefault: (name: string) => Promise<void>;
};

type Placed = {
  id: string;
  kind: NodeKind;
  label: string;
  detail: string;
  // x and y are the star's centre. The cell is the whole layout box, star and
  // label together, and it is what a fit frames so no label is cropped.
  x: number;
  y: number;
  cell: Box;
  phase: number;
};

// Gesture is what one pointer sequence is doing. A press on a star becomes a
// link once it travels; a press on empty space becomes a pan; anything under
// the travel threshold is a click.
type Gesture =
  | { kind: "idle" }
  | { kind: "press"; node?: string }
  | { kind: "pan" }
  | { kind: "link"; from: string }
  | { kind: "pinch"; startDistance: number; startScale: number; midX: number; midY: number };

const CLICK_TRAVEL = 6;

// One table for a star kind, so its tint cannot drift between the sprite and
// the selection ring.
const kindTint: Record<NodeKind, { css: string; hex: number }> = {
  client: { css: "#7dd3fc", hex: 0x7dd3fc },
  profile: { css: "#6ee7b7", hex: 0x6ee7b7 },
  upstream: { css: "#c4b5fd", hex: 0xc4b5fd },
  rule: { css: "#fcd34d", hex: 0xfcd34d },
};

const allowColor = 0x6ee7b7;
const blockColor = 0xfb7185;
// An edge is structure, so it is a desaturated slate rather than the client
// tint. Threading the sky colour through the graph made a crossing edge count
// as a client.
const lineColor = 0x7f8db0;
const labelColor = 0xeef2fa;
const detailColor = 0x8b96b5;

const fontStack = 'ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
const labelFont = `600 15px ${fontStack}`;
const detailFont = `400 12.5px ${fontStack}`;

// margin is the room a fit leaves around the constellation. The cells already
// hold their own labels, so this is breathing room rather than a label gutter.
const FIT_MARGIN = 40;

// margin is how close to the edge a revealed node may land.
const REVEAL_MARGIN = 60;

// The sprite is baked this large once. The biggest star is 150 world px, which
// the camera may draw at three times the scale on a 2x display, so a source
// below this point would be visibly magnified; above it, more pixels buy
// nothing on a soft glow.
const STAR_TEXTURE_SIZE = 512;

const elk = new ELK();

// displayResolution is the density the canvas renders at. It is clamped at 2
// because the fourth pixel of a 3x panel is not worth the fill rate.
function displayResolution(): number {
  return Math.min(window.devicePixelRatio || 1, 2);
}

// One offscreen context measures label widths, so the layout box can hold the
// text the graph will actually print instead of a guess at it.
let ruler: CanvasRenderingContext2D | undefined;

function measureText(text: string, font: string): number {
  ruler ??= document.createElement("canvas").getContext("2d") ?? undefined;
  if (!ruler) {
    return text.length * 7.6;
  }
  ruler.font = font;
  return ruler.measureText(text).width;
}

const measureWith = (font: string): Measure => (text: string) => measureText(text, font);

// makeStarTexture bakes one star: an atmosphere that reaches the tile edge, a
// chromosphere, a four-point flare, and a hot core. Every stop is rgba so the
// alpha and the tint stay independent.
function makeStarTexture(tint: string, flare: number): Texture {
  const size = STAR_TEXTURE_SIZE;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const context = canvas.getContext("2d");
  if (!context) {
    return Texture.WHITE;
  }
  const center = size / 2;
  const channels = [1, 3, 5].map((index) => parseInt(tint.slice(index, index + 2), 16));
  const rgba = (alpha: number) => `rgba(${channels[0]}, ${channels[1]}, ${channels[2]}, ${alpha})`;

  // The atmosphere. A falloff tuned so the tile edge is already clear, which
  // is what stops a star reading as a grey smudge.
  const atmosphere = context.createRadialGradient(center, center, 0, center, center, center);
  atmosphere.addColorStop(0, rgba(0.17));
  atmosphere.addColorStop(0.14, rgba(0.1));
  atmosphere.addColorStop(0.4, rgba(0.03));
  atmosphere.addColorStop(1, rgba(0));
  context.fillStyle = atmosphere;
  context.fillRect(0, 0, size, size);

  const chromosphere = context.createRadialGradient(center, center, 0, center, center, size * 0.15);
  chromosphere.addColorStop(0, rgba(0.9));
  chromosphere.addColorStop(0.28, rgba(0.52));
  chromosphere.addColorStop(0.62, rgba(0.14));
  chromosphere.addColorStop(1, rgba(0));
  context.fillStyle = chromosphere;
  context.fillRect(0, 0, size, size);

  // A tapered ray reads as a diffraction spike; a constant-width stroke reads
  // as a stick. Two long and two short is the cross flare.
  const ray = (angle: number, length: number, halfWidth: number, alpha: number) => {
    const dx = Math.cos(angle);
    const dy = Math.sin(angle);
    const nx = -dy;
    const ny = dx;
    const gradient = context.createLinearGradient(center, center, center + dx * length, center + dy * length);
    gradient.addColorStop(0, rgba(alpha));
    gradient.addColorStop(0.22, rgba(alpha * 0.4));
    gradient.addColorStop(0.6, rgba(alpha * 0.1));
    gradient.addColorStop(1, rgba(0));
    context.fillStyle = gradient;
    context.beginPath();
    context.moveTo(center + nx * halfWidth, center + ny * halfWidth);
    context.lineTo(center + dx * length, center + dy * length);
    context.lineTo(center - nx * halfWidth, center - ny * halfWidth);
    context.closePath();
    context.fill();
  };

  const long = size * flare;
  const short = long * 0.5;
  // The base half-width is what makes a ray visible. At a twentieth of a pixel
  // after the sprite is scaled down, a hairline is not a flare, it is noise.
  const base = size * 0.017;
  for (let index = 0; index < 4; index++) {
    const angle = (index * Math.PI) / 2;
    ray(angle, index % 2 === 0 ? long : short, base, 0.9);
  }
  for (let index = 0; index < 4; index++) {
    const angle = (index * Math.PI) / 2 + Math.PI / 4;
    ray(angle, short * 0.46, base * 0.55, 0.32);
  }

  const core = context.createRadialGradient(center, center, 0, center, center, size * 0.062);
  core.addColorStop(0, "rgba(255, 255, 255, 1)");
  core.addColorStop(0.32, "rgba(255, 255, 255, 0.94)");
  core.addColorStop(0.62, rgba(0.62));
  core.addColorStop(1, rgba(0));
  context.fillStyle = core;
  context.beginPath();
  context.arc(center, center, size * 0.062, 0, Math.PI * 2);
  context.fill();

  return Texture.from(canvas);
}

// The flare length as a fraction of the sprite, by kind. A rule is a small
// point of interest, an upstream is the brightest thing in the sky.
const kindFlare: Record<NodeKind, number> = {
  client: 0.3,
  profile: 0.34,
  upstream: 0.4,
  rule: 0.26,
};

// The sprite's drawn width, in world units, by kind.
const kindSize: Record<NodeKind, number> = {
  client: 124,
  profile: 140,
  upstream: 168,
  rule: 96,
};

export default function Graph(props: Props) {
  let host: HTMLDivElement | undefined;
  let app: Application | undefined;

  const [selected, setSelected] = createSignal<string>();
  const [graphError, setGraphError] = createSignal<string>();
  const [newProfile, setNewProfile] = createSignal("");
  const [creating, setCreating] = createSignal(false);
  const placed = new Map<string, Placed>();
  const flow: Flow = emptyFlow();

  let world: Container | undefined;
  let haloLayer: Container | undefined;
  let edgeLayer: Graphics | undefined;
  let nodeLayer: Container | undefined;
  let fxLayer: Graphics | undefined;
  let selection: Graphics | undefined;
  let linkRing: Graphics | undefined;
  let camera: Camera = { x: 0, y: 0, scale: 1 };
  // framed is whether the view has been placed once. A later redraw keeps the
  // camera, so adding a client does not throw away where the operator was
  // looking; the fit control is how they ask for it back.
  let framed = false;
  let starTextures: Map<NodeKind, Texture>;
  // reveal is the node a create should land on once the layout has placed it.
  let reveal: string | undefined;
  let draft: { from: string; x: number; y: number; target?: string; legal: boolean } | undefined;

  createEffect(
    () => [buildTopology(props.profiles, props.clients, props.defaultProfile, props.upstreams, props.rules)] as const,
    ([topology]) => {
      void draw(topology);
    },
  );

  createEffect(
    () => undefined,
    () => props.log.subscribe((decision) => route(decision)),
  );

  createEffect(
    () => undefined,
    () => {
      if (!host) {
        return;
      }
      const observer = new ResizeObserver(() => {
        if (!framed) {
          fitToView();
        }
      });
      observer.observe(host);
      onCleanup(() => observer.disconnect());
    },
  );

  onCleanup(() => {
    for (const texture of starTextures?.values() ?? []) {
      texture.destroy(true);
    }
    starTextures = new Map<NodeKind, Texture>();
    app?.destroy(true);
    app = undefined;
  });

  async function ensureApp(): Promise<Application> {
    if (app) {
      return app;
    }
    const instance = new Application();
    await instance.init({
      backgroundAlpha: 0,
      resizeTo: host,
      antialias: true,
      preserveDrawingBuffer: true,
      // Pixi renders at a resolution of 1 by default and then stretches the
      // canvas over its CSS box, so on a 2x display the stars, the labels, and
      // the flow were all drawn at half density and upscaled. autoDensity keeps
      // the CSS size while the backing store follows the display.
      resolution: displayResolution(),
      autoDensity: true,
    });
    host?.appendChild(instance.canvas);

    starTextures = new Map<NodeKind, Texture>(
      (Object.keys(kindTint) as NodeKind[]).map((kind) => [
        kind,
        makeStarTexture(kindTint[kind].css, kindFlare[kind]),
      ]),
    );
    world = new Container();
    haloLayer = new Container();
    edgeLayer = new Graphics();
    nodeLayer = new Container();
    fxLayer = new Graphics();
    selection = new Graphics();
    for (let index = 0; index < 4; index++) {
      selection.arc(0, 0, 17, (index * Math.PI) / 2 + 0.28, ((index + 1) * Math.PI) / 2 - 0.28);
      selection.stroke({ width: 1.4, color: 0xffffff, alpha: 0.85 });
    }
    linkRing = new Graphics();
    for (let index = 0; index < 4; index++) {
      linkRing.arc(0, 0, 20, (index * Math.PI) / 2 + 0.4, ((index + 1) * Math.PI) / 2 - 0.4);
      linkRing.stroke({ width: 2, color: 0xffffff, alpha: 0.9 });
    }
    linkRing.visible = false;
    world.addChild(edgeLayer, haloLayer, nodeLayer, fxLayer, selection, linkRing);
    instance.stage.addChild(world);
    instance.stage.eventMode = "static";
    instance.stage.hitArea = instance.screen;
    attachControls(instance);
    instance.ticker.add(() => {
      tick(instance.ticker.deltaMS / 1000);
    });

    app = instance;
    return instance;
  }

  async function draw(topology: Topology) {
    const instance = await ensureApp();

    const measureLabel = measureWith(labelFont);
    const measureDetail = measureWith(detailFont);
    const cells = topology.nodes.map((node) => {
      const label = fitLabel(node.label, measureLabel, LABEL_MAX);
      const detail = fitLabel(node.detail, measureDetail, LABEL_MAX);
      return {
        node,
        label,
        detail,
        width: cellWidth(measureLabel(label), measureDetail(detail)),
      };
    });

    const layout = await elk.layout({
      id: "root",
      layoutOptions: {
        "elk.algorithm": "layered",
        "elk.direction": "DOWN",
        "elk.spacing.nodeNode": "28",
        "elk.layered.spacing.nodeNodeBetweenLayers": "96",
      },
      children: cells.map((cell) => ({ id: cell.node.id, width: cell.width, height: ROW_HEIGHT })),
      edges: topology.edges.map((edge) => ({ id: edge.id, sources: [edge.from], targets: [edge.to] })),
    });

    placed.clear();
    for (const child of layout.children ?? []) {
      const cell = cells.find((candidate) => candidate.node.id === child.id);
      if (!cell) {
        continue;
      }
      const minX = child.x ?? 0;
      const minY = child.y ?? 0;
      placed.set(cell.node.id, {
        id: cell.node.id,
        kind: cell.node.kind,
        label: cell.label,
        detail: cell.detail,
        x: minX + STAR_CELL / 2,
        y: minY + ROW_HEIGHT / 2,
        cell: { minX, minY, maxX: minX + cell.width, maxY: minY + ROW_HEIGHT },
        phase: Math.random() * Math.PI * 2,
      });
    }

    edgeLayer?.clear();
    for (const edge of topology.edges) {
      const from = placed.get(edge.from);
      const to = placed.get(edge.to);
      if (!from || !to || !edgeLayer) {
        continue;
      }
      edgeLayer
        .moveTo(from.x, from.y)
        .lineTo(to.x, to.y)
        .stroke({ width: 1, color: lineColor, alpha: 0.26 });
    }

    haloLayer?.removeChildren();
    nodeLayer?.removeChildren();
    for (const node of placed.values()) {
      drawStar(node);
    }
    drawSelection();
    if (!framed) {
      fitToView();
    }
    // A reveal is a claim on a node a save has not placed yet. A draw whose
    // topology does not carry the node leaves the claim for the next one, so a
    // redraw that races the save cannot swallow it and drop the selection.
    if (reveal && topology.nodes.some((node) => node.id === reveal)) {
      const id = reveal;
      reveal = undefined;
      const node = placed.get(id);
      if (node && host) {
        setSelected(node.id);
        drawSelection();
        camera = ensureVisible(camera, node, host.clientWidth, host.clientHeight, REVEAL_MARGIN);
        applyCamera();
      }
    }
  }

  function drawStar(node: Placed) {
    if (!haloLayer || !nodeLayer) {
      return;
    }
    const size = kindSize[node.kind];
    const star = new Sprite(starTextures.get(node.kind) ?? Texture.WHITE);
    star.anchor.set(0.5);
    star.x = node.x;
    star.y = node.y;
    star.width = size;
    star.height = size;
    haloLayer.addChild(star);

    const anchorX = node.x + STAR_CELL / 2;
    // A shadow on the text is what keeps a label legible where it crosses the
    // atmosphere of its own star.
    const shadow = { color: 0x04060b, alpha: 0.85, blur: 4, distance: 1, angle: Math.PI / 2 };
    const label = new Text({
      text: node.label,
      style: {
        fill: labelColor,
        fontFamily: fontStack,
        fontSize: 15,
        fontWeight: "600",
        letterSpacing: 0.2,
        dropShadow: shadow,
      },
    });
    label.x = anchorX;
    label.y = node.y - 17;
    nodeLayer.addChild(label);

    const detail = new Text({
      text: node.detail,
      style: { fill: detailColor, fontFamily: fontStack, fontSize: 12.5, dropShadow: shadow },
    });
    detail.x = anchorX;
    detail.y = node.y + 2;
    nodeLayer.addChild(detail);
  }

  // fitToView frames the whole constellation, which is where a fresh page
  // lands and what the fit control asks for.
  function fitToView() {
    if (!host || placed.size === 0) {
      return;
    }
    let box: Box = { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity };
    for (const node of placed.values()) {
      box = {
        minX: Math.min(box.minX, node.cell.minX),
        minY: Math.min(box.minY, node.cell.minY),
        maxX: Math.max(box.maxX, node.cell.maxX),
        maxY: Math.max(box.maxY, node.cell.maxY),
      };
    }
    camera = frame(box, host.clientWidth, host.clientHeight, FIT_MARGIN);
    framed = true;
    applyCamera();
  }

  function drawSelection() {
    if (!selection) {
      return;
    }
    const node = selected() ? placed.get(selected() ?? "") : undefined;
    if (!selection) {
      return;
    }
    selection.visible = node !== undefined;
    if (!node) {
      return;
    }
    selection.position.set(node.x, node.y);
    selection.tint = kindTint[node.kind].hex;
  }

  // ── Live flow ─────────────────────────────────────────────────────────

  function route(decision: Decision) {
    const resolved = clientFor(decision.client ?? "", props.clients, props.defaultProfile);
    if (!resolved) {
      return;
    }
    const from = placed.get(resolved.clientID);
    const profile = placed.get(resolved.profileID);
    if (!from || !profile) {
      return;
    }
    const blocked = decision.action === "block";
    const upstream = blocked ? undefined : placed.get(upstreamNodeID(props.upstreams[0] ?? ""));
    if (!blocked && !upstream) {
      return;
    }
    if (!admit(flow, performance.now())) {
      return;
    }
    if (blocked) {
      flow.particles.push(streak([segment(from, profile)], blockColor, 240));
      flow.pulses.push({ x: profile.x, y: profile.y, age: 0, color: blockColor });
      return;
    }
    if (upstream) {
      flow.particles.push(streak([segment(from, profile), segment(profile, upstream)], allowColor, 200));
    }
  }

  function tick(deltaSeconds: number) {
    if (!fxLayer || !nodeLayer) {
      return;
    }
    fxLayer.clear();

    advance(flow, deltaSeconds);

    for (const particle of flow.particles) {
      const ends = trail(particle);
      fxLayer
        .moveTo(ends.from.x, ends.from.y)
        .lineTo(ends.to.x, ends.to.y)
        .stroke({ width: 2, color: particle.color, alpha: 0.35, cap: "round" });
      fxLayer.circle(ends.to.x, ends.to.y, 2.4).fill(particle.color);
    }

    for (const pulse of flow.pulses) {
      const ratio = pulse.age / PULSE_LIFE_SECONDS;
      fxLayer
        .circle(pulse.x, pulse.y, 10 + ratio * 26)
        .stroke({ width: 1.6 * (1 - ratio), color: pulse.color, alpha: 1 - ratio });
    }

    if (draft) {
      const origin = placed.get(draft.from);
      if (origin) {
        fxLayer
          .moveTo(origin.x, origin.y)
          .lineTo(draft.x, draft.y)
          .stroke({ width: 1.6, color: draft.legal ? allowColor : blockColor, alpha: 0.8, cap: "round" });
        fxLayer.circle(draft.x, draft.y, 3).fill({ color: draft.legal ? allowColor : blockColor, alpha: 0.9 });
      }
    }

    const time = performance.now() / 1000;
    const nodes = [...placed.values()];
    haloLayer?.children.forEach((child, index) => {
      const node = nodes[index];
      if (node) {
        // A shallow pulse. The old swing took a star down to a third of its
        // brightness, which read as flicker rather than as a sky.
        child.alpha = 0.66 + Math.sin(time * 0.9 + node.phase) * 0.08;
      }
    });
    if (selection) {
      selection.alpha = 0.55 + Math.sin(time * 2.4) * 0.35;
      selection.rotation += deltaSeconds * 0.7;
    }
  }

  function attachControls(instance: Application) {
    let gesture: Gesture = { kind: "idle" };
    let moved = 0;
    let last = { x: 0, y: 0 };
    const pointers = new Map<number, { x: number; y: number }>();

    const pinchState = (startScale: number) => {
      const points = [...pointers.values()];
      if (points.length < 2) {
        return undefined;
      }
      const [a, b] = points;
      return {
        startDistance: Math.hypot(a.x - b.x, a.y - b.y),
        startScale,
        midX: (a.x + b.x) / 2,
        midY: (a.y + b.y) / 2,
      };
    };

    instance.stage.on("pointerdown", (event) => {
      pointers.set(event.pointerId, { x: event.global.x, y: event.global.y });
      moved = 0;
      last = { x: event.global.x, y: event.global.y };
      if (pointers.size === 2) {
        const start = pinchState(camera.scale);
        if (start) {
          gesture = { kind: "pinch", ...start };
        }
        draft = undefined;
        drawDraft();
        return;
      }
      gesture = { kind: "press", node: pick(event.global.x, event.global.y) };
    });

    instance.stage.on("pointermove", (event) => {
      if (!pointers.has(event.pointerId)) {
        return;
      }
      pointers.set(event.pointerId, { x: event.global.x, y: event.global.y });
      if (gesture.kind === "pinch") {
        const current = pinchState(gesture.startScale);
        if (!current || current.startDistance === 0) {
          return;
        }
        camera = zoomAt(
          { x: camera.x, y: camera.y, scale: gesture.startScale },
          current.startDistance / gesture.startDistance,
          current.midX,
          current.midY,
        );
        applyCamera();
        return;
      }
      const dx = event.global.x - last.x;
      const dy = event.global.y - last.y;
      moved += Math.abs(dx) + Math.abs(dy);
      last = { x: event.global.x, y: event.global.y };
      if (gesture.kind === "press") {
        if (moved < CLICK_TRAVEL) {
          return;
        }
        gesture = gesture.node ? { kind: "link", from: gesture.node } : { kind: "pan" };
      }
      if (gesture.kind === "pan") {
        camera = pan(camera, dx, dy);
        applyCamera();
        return;
      }
      if (gesture.kind === "link") {
        updateDraft(gesture.from, event.global.x, event.global.y);
      }
    });

    const release = (event: { pointerId: number; global: { x: number; y: number } }) => {
      pointers.delete(event.pointerId);
      if (gesture.kind === "pinch" && pointers.size < 2) {
        gesture = { kind: "pan" };
      }
      if (gesture.kind === "press") {
        if (moved < CLICK_TRAVEL) {
          setSelected(pick(event.global.x, event.global.y));
          drawSelection();
        }
        gesture = { kind: "idle" };
      } else if (gesture.kind === "link") {
        const target = pick(event.global.x, event.global.y);
        if (target && target !== gesture.from) {
          void applyLink(gesture.from, target);
        }
        draft = undefined;
        drawDraft();
        gesture = { kind: "idle" };
      }
      if (pointers.size === 0 && gesture.kind === "pan") {
        gesture = { kind: "idle" };
      }
    };

    instance.stage.on("pointerup", release);
    instance.stage.on("pointerupoutside", release);

    const canvas = instance.canvas;
    canvas.addEventListener("wheel", (event) => {
      event.preventDefault();
      const factor = Math.pow(1.0015, -event.deltaY);
      camera = zoomAt(camera, factor, event.offsetX, event.offsetY);
      applyCamera();
    });
  }

  function pick(screenX: number, screenY: number): string | undefined {
    const { x: worldX, y: worldY } = toWorld(camera, screenX, screenY);
    let best: { id: string; distance: number } | undefined;
    for (const node of placed.values()) {
      const distance = Math.hypot(node.x - worldX, node.y - worldY);
      if (distance < 26 && (!best || distance < best.distance)) {
        best = { id: node.id, distance };
      }
    }
    return best?.id;
  }

  function applyCamera() {
    if (!world) {
      return;
    }
    world.position.set(camera.x, camera.y);
    world.scale.set(camera.scale);
  }

  function updateDraft(from: string, screenX: number, screenY: number) {
    const origin = placed.get(from);
    if (!origin) {
      return;
    }
    const cursor = toWorld(camera, screenX, screenY);
    const target = pick(screenX, screenY);
    const legal =
      target !== undefined && target !== from && linkIntent([...placed.values()], from, target) !== undefined;
    draft = { from, x: cursor.x, y: cursor.y, target: target === from ? undefined : target, legal };
    drawDraft();
  }

  function drawDraft() {
    if (!linkRing) {
      return;
    }
    const node = draft?.target ? placed.get(draft.target) : undefined;
    linkRing.visible = node !== undefined;
    if (node) {
      linkRing.position.set(node.x, node.y);
      linkRing.tint = draft?.legal ? allowColor : blockColor;
    }
  }

  // applyLink writes one drag through the endpoint that owns the record, so
  // validation stays on the server and a rejected link changes nothing.
  async function applyLink(from: string, to: string) {
    const intent = linkIntent([...placed.values()], from, to);
    if (!intent) {
      return;
    }
    setGraphError(undefined);
    try {
      if (intent.op === "reassign") {
        const client = props.clients.find((candidate) => candidate.name === intent.client);
        if (!client) {
          return;
        }
        await props.onSaveClient(intent.client, {
          profile: intent.profile,
          notes: client.notes,
          addresses: client.addresses,
          macs: client.macs,
          prefixes: client.prefixes,
        });
      } else if (intent.op === "default") {
        await props.onSetDefault(intent.profile);
      } else {
        const child = props.profiles.find((candidate) => candidate.name === intent.child);
        if (!child) {
          return;
        }
        await props.onSaveProfile(intent.child, {
          extends: intent.parent,
          mode: child.mode ?? "",
          custom: child.custom ?? "",
        });
      }
      setSelected(to);
      drawSelection();
    } catch (cause) {
      setGraphError(String(cause));
    }
  }

  async function createProfile() {
    const name = newProfile().trim();
    if (!name) {
      return;
    }
    if (props.profiles.some((profile) => profile.name === name)) {
      setGraphError(`a profile named ${name} already exists`);
      return;
    }
    setCreating(true);
    setGraphError(undefined);
    // reveal is claimed before the write so the redraw the refresh triggers
    // cannot land between the save and the claim.
    reveal = `profile:${name}`;
    try {
      await props.onSaveProfile(name, {});
      setNewProfile("");
    } catch (cause) {
      setGraphError(String(cause));
    } finally {
      setCreating(false);
    }
  }

  // ── Selection panel ───────────────────────────────────────────────────

  const selectedNode = () => {
    const id = selected();
    return id ? placed.get(id) : undefined;
  };

  const selectedClient = () => {
    const node = selectedNode();
    if (!node || node.kind !== "client" || node.id === "client:unidentified") {
      return undefined;
    }
    return props.clients.find((client) => client.name === node.label);
  };

  const selectedProfile = () => {
    const node = selectedNode();
    if (!node || node.kind !== "profile") {
      return undefined;
    }
    return props.profiles.find((profile) => profile.name === node.label);
  };

  const selectedRule = () => {
    const node = selectedNode();
    if (!node || node.kind !== "rule") {
      return undefined;
    }
    return props.rules.find((rule) => `rule:${rule.id}` === node.id);
  };

  return (
    <div
      class="graph"
      data-testid="graph"
      ref={(element) => {
        host = element as HTMLDivElement;
      }}
    >
      <span class="hint">click a star · drag star to star to connect · scroll to zoom</span>
      <form
        class="graph-create"
        onSubmit={(event) => {
          event.preventDefault();
          void createProfile();
        }}
      >
        <input
          data-testid="graph-create-name"
          placeholder="new profile"
          value={newProfile()}
          onInput={(event) => setNewProfile(event.currentTarget.value)}
        />
        <button type="submit" class="btn-ghost" data-testid="graph-create" disabled={!newProfile().trim() || creating()}>
          Add
        </button>
      </form>
      <Show when={graphError()}>
        <p class="error graph-error" data-testid="graph-error">
          {graphError()}
        </p>
      </Show>
      <button type="button" class="btn-ghost fit" data-testid="graph-fit" onClick={() => fitToView()}>
        Fit
      </button>
      <Show when={selectedClient()}>
        {(client) => (
          <div class="graph-panel" data-testid="graph-panel">
            <header>
              <h2>{client().name}</h2>
              <button type="button" class="btn-ghost" onClick={() => setSelected(undefined)}>
                ×
              </button>
            </header>
            <ClientPanel
              client={client()}
              profiles={props.profiles}
              onSave={async (profile) => {
                await props.onSaveClient(client().name, {
                  profile,
                  notes: client().notes,
                  addresses: client().addresses,
                  macs: client().macs,
                  prefixes: client().prefixes,
                });
                setSelected(undefined);
              }}
            />
          </div>
        )}
      </Show>
      <Show when={selectedProfile()}>
        {(profile) => (
          <div class="graph-panel" data-testid="graph-panel">
            <header>
              <h2>{profile().name}</h2>
              <button type="button" class="btn-ghost" onClick={() => setSelected(undefined)}>
                ×
              </button>
            </header>
            <div class="graph-panel-body">
              <p class="muted">answers {effectiveMode(props.profiles, profile().name)}</p>
              <p class="muted">
                {props.clients.filter((client) => client.profile === profile().name).length} clients bound
              </p>
              <ProfilePanel
                profile={profile()}
                onSave={async (input) => {
                  await props.onSaveProfile(profile().name, input);
                }}
              />
              <Show when={props.defaultProfile !== profile().name}>
                <button type="button" class="btn" onClick={() => void props.onSetDefault(profile().name)}>
                  Make default
                </button>
              </Show>
            </div>
          </div>
        )}
      </Show>
      <Show when={selectedRule()}>
        {(rule) => (
          <div class="graph-panel" data-testid="graph-panel">
            <header>
              <h2>{rule().domain}</h2>
              <button type="button" class="btn-ghost" onClick={() => setSelected(undefined)}>
                ×
              </button>
            </header>
            <div class="graph-panel-body">
              <p class="muted">
                {rule().action} · {rule().kind}
              </p>
              <Show when={rule().client}>
                <p class="muted">client {rule().client}</p>
              </Show>
              <Show when={rule().schedule}>
                <p class="muted">schedule {rule().schedule}</p>
              </Show>
              <Show when={rule().notes}>
                <p class="muted">{rule().notes}</p>
              </Show>
            </div>
          </div>
        )}
      </Show>
    </div>
  );
}

function ProfilePanel(props: { profile: Profile; onSave: (input: ProfileInput) => Promise<void> }) {
  const [mode, setMode] = createSignal(props.profile.mode ?? "");
  const [custom, setCustom] = createSignal(props.profile.custom ?? "");
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string>();

  const untouched = () => mode() === (props.profile.mode ?? "") && custom() === (props.profile.custom ?? "");

  async function save() {
    setBusy(true);
    setError(undefined);
    try {
      await props.onSave({ extends: props.profile.extends ?? "", mode: mode(), custom: custom() });
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <label>
        Blocking mode
        <select data-testid="graph-panel-mode" value={mode()} onInput={(event) => setMode(event.currentTarget.value)}>
          <option value="">inherit</option>
          <For each={BLOCKING_MODES}>{(value) => <option value={value}>{value}</option>}</For>
        </select>
      </label>
      <Show when={mode() === "custom-address"}>
        <label>
          Custom address
          <input
            data-testid="graph-panel-custom"
            value={custom()}
            placeholder="10.0.0.1"
            onInput={(event) => setCustom(event.currentTarget.value)}
          />
        </label>
      </Show>
      <button
        type="button"
        class="btn-solid"
        data-testid="graph-panel-mode-save"
        disabled={busy() || untouched()}
        onClick={() => void save()}
      >
        Save mode
      </button>
      {error() ? <p class="error">{error()}</p> : null}
    </div>
  );
}

function ClientPanel(props: { client: Client; profiles: Profile[]; onSave: (profile: string) => Promise<void> }) {
  const [profile, setProfile] = createSignal(props.client.profile);
  const [busy, setBusy] = createSignal(false);
  return (
    <div class="graph-panel-body">
      <label>
        Policy profile
        <select data-testid="graph-panel-profile" value={profile()} onInput={(event) => setProfile(event.currentTarget.value)}>
          {props.profiles.map((item) => (
            <option value={item.name}>{item.name}</option>
          ))}
        </select>
      </label>
      <button
        type="button"
        class="btn-solid"
        data-testid="graph-panel-save"
        disabled={busy() || profile() === props.client.profile}
        onClick={() => {
          setBusy(true);
          void props.onSave(profile()).finally(() => setBusy(false));
        }}
      >
        Save policy
      </button>
    </div>
  );
}
