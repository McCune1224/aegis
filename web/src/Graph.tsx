import ELK, { type ElkNode } from "elkjs/lib/elk.bundled.js";
import { Application, Container, Graphics, Sprite, Text, Texture } from "pixi.js";
import { createEffect, createSignal, onCleanup, Show } from "solid-js";
import type { Client, Profile } from "./api";
import type { Decision } from "./api";
import { effectiveMode } from "./resolve";
import type { QueryLog } from "./querylog";
import { buildTopology, matchClient, upstreamNodeID, type Topology } from "./topology";

type Props = {
  profiles: Profile[];
  clients: Client[];
  defaultProfile: string;
  upstreams: string[];
  log: QueryLog;
  onSaveClient: (name: string, input: { profile: string; notes: string; addresses: string[]; macs: string[]; prefixes: string[] }) => Promise<void>;
  onSetDefault: (name: string) => Promise<void>;
};

type Kind = "client" | "profile" | "upstream";

type Placed = {
  id: string;
  kind: Kind;
  label: string;
  detail: string;
  x: number;
  y: number;
  phase: number;
};

type Segment = { from: { x: number; y: number }; to: { x: number; y: number } };

type Particle = {
  segments: Segment[];
  travelled: number;
  length: number;
  speed: number;
  color: number;
};

type Pulse = { x: number; y: number; age: number; color: number };

const kindColor: Record<Kind, number> = {
  client: 0x7dd3fc,
  profile: 0x6ee7b7,
  upstream: 0xc4b5fd,
};

const allowColor = 0x6ee7b7;
const blockColor = 0xfb7185;
const lineColor = 0x7dd3fc;
const labelColor = 0xe9effc;
const detailColor = 0x8b96b5;

const minScale = 0.4;
const maxScale = 3;

const elk = new ELK();

// makeStarTexture bakes one star: a soft outer halo, a colored inner glow,
// four diffraction spikes, and a hot core. Tint arrives as an rgb hex string
// like "#7dd3fc" so the gradient stops can carry alpha.
function makeStarTexture(tint: string, spikes: number): Texture {
  const size = 160;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const context = canvas.getContext("2d");
  if (!context) {
    return Texture.WHITE;
  }
  const center = size / 2;

  const halo = context.createRadialGradient(center, center, 0, center, center, center);
  halo.addColorStop(0, `${tint}55`);
  halo.addColorStop(0.4, `${tint}22`);
  halo.addColorStop(1, `${tint}00`);
  context.fillStyle = halo;
  context.fillRect(0, 0, size, size);

  const glow = context.createRadialGradient(center, center, 0, center, center, 26);
  glow.addColorStop(0, `${tint}cc`);
  glow.addColorStop(0.6, `${tint}55`);
  glow.addColorStop(1, `${tint}00`);
  context.fillStyle = glow;
  context.beginPath();
  context.arc(center, center, 26, 0, Math.PI * 2);
  context.fill();

  context.lineCap = "round";
  for (let index = 0; index < 4; index++) {
    const angle = (index * Math.PI) / 2;
    const inner = 5;
    const outer = index % 2 === 0 ? spikes : spikes * 0.55;
    const gradient = context.createLinearGradient(
      center + Math.cos(angle) * inner,
      center + Math.sin(angle) * inner,
      center + Math.cos(angle) * outer,
      center + Math.sin(angle) * outer,
    );
    gradient.addColorStop(0, `${tint}dd`);
    gradient.addColorStop(1, `${tint}00`);
    context.strokeStyle = gradient;
    context.lineWidth = 2.2;
    context.beginPath();
    context.moveTo(center + Math.cos(angle) * inner, center + Math.sin(angle) * inner);
    context.lineTo(center + Math.cos(angle) * outer, center + Math.sin(angle) * outer);
    context.stroke();
  }

  const core = context.createRadialGradient(center, center, 0, center, center, 7);
  core.addColorStop(0, "#ffffff");
  core.addColorStop(0.5, "#ffffffee");
  core.addColorStop(1, "#ffffff00");
  context.fillStyle = core;
  context.beginPath();
  context.arc(center, center, 7, 0, Math.PI * 2);
  context.fill();

  return Texture.from(canvas);
}

const kindTint: Record<Kind, string> = {
  client: "#7dd3fc",
  profile: "#6ee7b7",
  upstream: "#c4b5fd",
};

export default function Graph(props: Props) {
  let host: HTMLDivElement | undefined;
  let app: Application | undefined;

  const [selected, setSelected] = createSignal<string>();
  const placed = new Map<string, Placed>();
  const particles: Particle[] = [];
  const pulses: Pulse[] = [];

  let world: Container | undefined;
  let haloLayer: Container | undefined;
  let edgeLayer: Graphics | undefined;
  let nodeLayer: Container | undefined;
  let fxLayer: Graphics | undefined;
  let selection: Graphics | undefined;
  let camera = { x: 0, y: 0, scale: 1 };
  let starTextures: Map<Kind, Texture>;

  createEffect(
    () => [buildTopology(props.profiles, props.clients, props.defaultProfile, props.upstreams)] as const,
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
      const observer = new ResizeObserver(() => fitToView());
      observer.observe(host);
      onCleanup(() => observer.disconnect());
    },
  );

  onCleanup(() => {
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
    });
    host?.appendChild(instance.canvas);

    starTextures = new Map<Kind, Texture>([
      ["client", makeStarTexture(kindTint.client, 44)],
      ["profile", makeStarTexture(kindTint.profile, 56)],
      ["upstream", makeStarTexture(kindTint.upstream, 68)],
    ]);
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
    world.addChild(edgeLayer, haloLayer, nodeLayer, fxLayer, selection);
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
    const layout = await elk.layout({
      id: "root",
      layoutOptions: {
        "elk.algorithm": "layered",
        "elk.direction": "DOWN",
        "elk.spacing.nodeNode": "40",
        "elk.layered.spacing.nodeNodeBetweenLayers": "120",
      },
      children: topology.nodes.map((node) => ({ id: node.id, width: 120, height: 44 })),
      edges: topology.edges.map((edge) => ({ id: edge.id, sources: [edge.from], targets: [edge.to] })),
    });

    placed.clear();
    for (const child of layout.children ?? []) {
      const node = topology.nodes.find((candidate) => candidate.id === child.id);
      if (!node) {
        continue;
      }
      placed.set(node.id, {
        id: node.id,
        kind: node.kind,
        label: node.label,
        detail: node.detail,
        x: (child.x ?? 0) + 60,
        y: (child.y ?? 0) + 22,
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
        .stroke({ width: 1, color: lineColor, alpha: 0.16 });
    }

    haloLayer?.removeChildren();
    nodeLayer?.removeChildren();
    for (const node of placed.values()) {
      drawStar(node);
    }
    drawSelection();
    fitToView();
  }

  function drawStar(node: Placed) {
    if (!haloLayer || !nodeLayer) {
      return;
    }
    const size = node.kind === "upstream" ? 150 : node.kind === "profile" ? 124 : 108;
    const star = new Sprite(starTextures.get(node.kind) ?? Texture.WHITE);
    star.anchor.set(0.5);
    star.x = node.x;
    star.y = node.y;
    star.width = size;
    star.height = size;
    haloLayer.addChild(star);

    const label = new Text({
      text: node.label,
      style: { fill: labelColor, fontSize: 13, fontWeight: "600", letterSpacing: 0.5 },
    });
    label.x = node.x + 16;
    label.y = node.y - 12;
    nodeLayer.addChild(label);

    const detail = new Text({ text: node.detail, style: { fill: detailColor, fontSize: 10.5 } });
    detail.x = node.x + 16;
    detail.y = node.y + 4;
    nodeLayer.addChild(detail);
  }

  // fitToView centers the constellation in the viewport on every layout, so a
  // fresh page lands with the whole sky in frame.
  function fitToView() {
    if (!host || placed.size === 0) {
      return;
    }
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (const node of placed.values()) {
      minX = Math.min(minX, node.x);
      minY = Math.min(minY, node.y);
      maxX = Math.max(maxX, node.x);
      maxY = Math.max(maxY, node.y);
    }
    const width = host.clientWidth;
    const height = host.clientHeight;
    const contentWidth = maxX - minX + 220;
    const contentHeight = maxY - minY + 160;
    camera.scale = Math.min(1.4, Math.min(width / contentWidth, height / contentHeight));
    camera.x = (width - (minX + maxX) * camera.scale) / 2;
    camera.y = (height - (minY + maxY) * camera.scale) / 2;
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
    selection.tint = kindColor[node.kind];
  }

  // ── Live flow ─────────────────────────────────────────────────────────

  function route(decision: Decision) {
    const profileName = resolveProfile(decision.address);
    if (!profileName) {
      return;
    }
    const from = placed.get(`client:${profileName.clientName}`);
    const profile = placed.get(`profile:${profileName.profile}`);
    if (!from || !profile) {
      return;
    }
    const blocked = decision.action === "block";
    if (blocked) {
      particles.push(streak([segment(from, profile)], blockColor, 240));
      pulses.push({ x: profile.x, y: profile.y, age: 0, color: blockColor });
      return;
    }
    const upstream = placed.get(upstreamNodeID(props.upstreams[0] ?? ""));
    if (!upstream) {
      return;
    }
    particles.push(streak([segment(from, profile), segment(profile, upstream)], allowColor, 200));
  }

  function resolveProfile(address: string): { clientName: string; profile: string } | undefined {
    const id = matchClient(props.clients, address);
    if (id) {
      const name = id.slice("client:".length);
      const client = props.clients.find((candidate) => candidate.name === name);
      return client ? { clientName: client.name, profile: client.profile } : undefined;
    }
    if (props.defaultProfile) {
      return { clientName: "unidentified", profile: props.defaultProfile };
    }
    return undefined;
  }

  function streak(segments: Segment[], color: number, speed: number): Particle {
    return {
      segments,
      travelled: 0,
      length: segments.reduce((total, current) => total + distance(current), 0),
      speed,
      color,
    };
  }

  function segment(from: Placed, to: Placed): Segment {
    return { from: { x: from.x, y: from.y }, to: { x: to.x, y: to.y } };
  }

  function distance(segment: Segment): number {
    const dx = segment.to.x - segment.from.x;
    const dy = segment.to.y - segment.from.y;
    return Math.hypot(dx, dy);
  }

  function pointAt(segments: Segment[], travelled: number): { x: number; y: number } {
    let remaining = travelled;
    for (const segment of segments) {
      const length = distance(segment);
      if (remaining <= length) {
        const ratio = length === 0 ? 0 : remaining / length;
        return {
          x: segment.from.x + (segment.to.x - segment.from.x) * ratio,
          y: segment.from.y + (segment.to.y - segment.from.y) * ratio,
        };
      }
      remaining -= length;
    }
    return segments[segments.length - 1]?.to ?? { x: 0, y: 0 };
  }

  function tick(deltaSeconds: number) {
    if (!fxLayer || !nodeLayer) {
      return;
    }
    fxLayer.clear();

    particles.forEach((particle) => {
      particle.travelled += particle.speed * deltaSeconds;
    });
    for (let index = particles.length - 1; index >= 0; index--) {
      const particle = particles[index];
      if (particle.travelled > particle.length + 24) {
        particles.splice(index, 1);
      }
    }
    for (const particle of particles) {
      const trailFrom = pointAt(particle.segments, Math.max(0, particle.travelled - 26));
      const head = pointAt(particle.segments, Math.min(particle.travelled, particle.length));
      fxLayer
        .moveTo(trailFrom.x, trailFrom.y)
        .lineTo(head.x, head.y)
        .stroke({ width: 2, color: particle.color, alpha: 0.35, cap: "round" });
      fxLayer.circle(head.x, head.y, 2.4).fill(particle.color);
    }

    for (let index = pulses.length - 1; index >= 0; index--) {
      const pulse = pulses[index];
      pulse.age += deltaSeconds;
      if (pulse.age > 0.5) {
        pulses.splice(index, 1);
        continue;
      }
      const ratio = pulse.age / 0.5;
      fxLayer
        .circle(pulse.x, pulse.y, 10 + ratio * 26)
        .stroke({ width: 1.6 * (1 - ratio), color: pulse.color, alpha: 1 - ratio });
    }

    const time = performance.now() / 1000;
    haloLayer?.children.forEach((child, index) => {
      const node = [...placed.values()][index];
      if (node) {
        child.alpha = 0.45 + Math.sin(time * 0.9 + node.phase) * 0.12;
      }
    });
    if (selection) {
      selection.alpha = 0.55 + Math.sin(time * 2.4) * 0.35;
      selection.rotation += deltaSeconds * 0.7;
    }
  }

  // ── Pan, zoom, selection ──────────────────────────────────────────────

  function attachControls(instance: Application) {
    let moved = 0;
    let last = { x: 0, y: 0 };
    const pointers = new Map<number, { x: number; y: number }>();
    let pinch: { startDistance: number; startScale: number; midX: number; midY: number } | undefined;

    const pinchState = () => {
      const points = [...pointers.values()];
      if (points.length < 2) {
        return undefined;
      }
      const [a, b] = points;
      return {
        startDistance: Math.hypot(a.x - b.x, a.y - b.y),
        startScale: camera.scale,
        midX: (a.x + b.x) / 2,
        midY: (a.y + b.y) / 2,
      };
    };

    instance.stage.on("pointerdown", (event) => {
      pointers.set(event.pointerId, { x: event.global.x, y: event.global.y });
      moved = 0;
      last = { x: event.global.x, y: event.global.y };
      if (pointers.size === 2) {
        pinch = pinchState();
      }
    });

    instance.stage.on("pointermove", (event) => {
      if (!pointers.has(event.pointerId)) {
        return;
      }
      pointers.set(event.pointerId, { x: event.global.x, y: event.global.y });
      if (pointers.size >= 2 && pinch) {
        const current = pinchState();
        if (!current || current.startDistance === 0) {
          return;
        }
        const next = Math.min(maxScale, Math.max(minScale, pinch.startScale * (current.startDistance / pinch.startDistance)));
        const applied = next / camera.scale;
        camera.x = pinch.midX - applied * (pinch.midX - camera.x);
        camera.y = pinch.midY - applied * (pinch.midY - camera.y);
        camera.scale = next;
        applyCamera();
        return;
      }
      const dx = event.global.x - last.x;
      const dy = event.global.y - last.y;
      moved += Math.abs(dx) + Math.abs(dy);
      camera.x += dx;
      camera.y += dy;
      last = { x: event.global.x, y: event.global.y };
      applyCamera();
    });

    const release = (event: { pointerId: number; global: { x: number; y: number } }) => {
      pointers.delete(event.pointerId);
      if (pointers.size < 2) {
        pinch = undefined;
      }
      if (moved < 6) {
        setSelected(pick(event.global.x, event.global.y));
        drawSelection();
      }
    };

    instance.stage.on("pointerup", release);
    instance.stage.on("pointerupoutside", release);

    const canvas = instance.canvas;
    canvas.addEventListener("wheel", (event) => {
      event.preventDefault();
      const factor = Math.pow(1.0015, -event.deltaY);
      const next = Math.min(maxScale, Math.max(minScale, camera.scale * factor));
      const applied = next / camera.scale;
      camera.x = event.offsetX - applied * (event.offsetX - camera.x);
      camera.y = event.offsetY - applied * (event.offsetY - camera.y);
      camera.scale = next;
      applyCamera();
    });
  }

  function pick(screenX: number, screenY: number): string | undefined {
    const worldX = (screenX - camera.x) / camera.scale;
    const worldY = (screenY - camera.y) / camera.scale;
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

  return (
    <div
      class="graph"
      data-testid="graph"
      ref={(element) => {
        host = element as HTMLDivElement;
      }}
    >
      <span class="hint">drag to pan · scroll to zoom · click a star</span>
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
              <Show when={props.defaultProfile !== profile().name}>
                <button type="button" class="btn" onClick={() => void props.onSetDefault(profile().name)}>
                  Make default
                </button>
              </Show>
            </div>
          </div>
        )}
      </Show>
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
