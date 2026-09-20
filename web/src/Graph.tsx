import ELK, { type ElkNode } from "elkjs/lib/elk.bundled.js";
import { Application, Container, Graphics, Sprite, Text, Texture } from "pixi.js";
import { createEffect, createSignal, onCleanup, Show } from "solid-js";
import type { Client, Profile } from "./api";
import { frame, pan, toWorld, zoomAt, type Box, type Camera } from "./camera";
import type { Decision } from "./api";
import { effectiveMode } from "./resolve";
import type { QueryLog } from "./querylog";
import { buildTopology, clientFor, upstreamNodeID, type Topology } from "./topology";
import { admit, advance, emptyFlow, PULSE_LIFE_SECONDS, segment, streak, trail, type Flow } from "./flow";

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

// margin is the room a fit leaves around the stars for their labels.
const FIT_MARGIN = 220;

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
  const flow: Flow = emptyFlow();

  let world: Container | undefined;
  let haloLayer: Container | undefined;
  let edgeLayer: Graphics | undefined;
  let nodeLayer: Container | undefined;
  let fxLayer: Graphics | undefined;
  let selection: Graphics | undefined;
  let camera: Camera = { x: 0, y: 0, scale: 1 };
  // framed is whether the view has been placed once. A later redraw keeps the
  // camera, so adding a client does not throw away where the operator was
  // looking; the fit control is how they ask for it back.
  let framed = false;
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
    if (!framed) {
      fitToView();
    }
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

  // fitToView frames the whole constellation, which is where a fresh page
  // lands and what the fit control asks for.
  function fitToView() {
    if (!host || placed.size === 0) {
      return;
    }
    let box: Box = { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity };
    for (const node of placed.values()) {
      box = {
        minX: Math.min(box.minX, node.x),
        minY: Math.min(box.minY, node.y),
        maxX: Math.max(box.maxX, node.x),
        maxY: Math.max(box.maxY, node.y),
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
    selection.tint = kindColor[node.kind];
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

    const time = performance.now() / 1000;
    const nodes = [...placed.values()];
    haloLayer?.children.forEach((child, index) => {
      const node = nodes[index];
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
        camera = zoomAt(
          { x: camera.x, y: camera.y, scale: pinch.startScale },
          current.startDistance / pinch.startDistance,
          pinch.midX,
          pinch.midY,
        );
        applyCamera();
        return;
      }
      const dx = event.global.x - last.x;
      const dy = event.global.y - last.y;
      moved += Math.abs(dx) + Math.abs(dy);
      camera = pan(camera, dx, dy);
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
