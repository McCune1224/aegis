import ELK from "elkjs/lib/elk.bundled.js";
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
   // x and y are the node's centre. The cell is the whole layout box, marker
   // and label together, and it is what a fit frames so no label is cropped.
   x: number;
   y: number;
   cell: Box;
   phase: number;
};

// Gesture is what one pointer sequence is doing. A press on a node becomes a
// link once it travels; a press on empty space becomes a pan; anything under
// the travel threshold is a click.
type Gesture =
   | { kind: "idle" }
   | { kind: "press"; node?: string }
   | { kind: "pan" }
   | { kind: "link"; from: string }
   | { kind: "pinch"; startDistance: number; startScale: number; midX: number; midY: number };

const CLICK_TRAVEL = 6;

const INK = "#141414";
const INK_SOFT = "#8b8a83";
const RED = "#e32119";
const GREEN = "#157a3b";
const PAPER = "#f4f3ee";
const allowColor = 0x157a3b;
const blockColor = 0xe32119;

// Marker geometry by kind, in world units. The shape carries the kind, the
// label carries the name, red carries only selection and blocking.
const MARKER: Record<NodeKind, number> = {
   client: 18,
   profile: 20,
   upstream: 26,
   rule: 12,
};

const fontStack = 'ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';

// margin is the room a fit leaves around the graph. The cells already hold
// their own labels, so this is breathing room rather than a label gutter.
const FIT_MARGIN = 40;

// margin is how close to the edge a revealed node may land.
const REVEAL_MARGIN = 60;

const elk = new ELK();

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

const hex = (color: number): string => `#${color.toString(16).padStart(6, "0")}`;

// The SVG namespace, for the imperative layers. Layout, edges, nodes, and
// effects are rebuilt per draw rather than diffed: the graph owns its geometry.
const SVG_NS = "http://www.w3.org/2000/svg";

function el(name: string, attributes: Record<string, string | number>): SVGElement {
   const element = document.createElementNS(SVG_NS, name);
   for (const [key, value] of Object.entries(attributes)) {
      element.setAttribute(key, String(value));
   }
   return element;
}

export default function Graph(props: Props) {
   let host: HTMLDivElement | undefined;
   let svg: SVGSVGElement | undefined;
   let world: SVGGElement | undefined;
   let edgeLayer: SVGGElement | undefined;
   let nodeLayer: SVGGElement | undefined;
   let fxLayer: SVGGElement | undefined;
   let linkRingBox: SVGRectElement | undefined;

   const [selected, setSelected] = createSignal<string>();
   const [graphError, setGraphError] = createSignal<string>();
   const [newProfile, setNewProfile] = createSignal("");
   const [creating, setCreating] = createSignal(false);
   const [zoom, setZoom] = createSignal(1);
   const placed = new Map<string, Placed>();
   const flow: Flow = emptyFlow();

   let camera: Camera = { x: 0, y: 0, scale: 1 };
   // framed is whether the view has been placed once. A later redraw keeps the
   // camera, so adding a client does not throw away where the operator was
   // looking; the fit control is how they ask for it back.
   let framed = false;
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
            sizeSvg();
            if (!framed) {
               fitToView();
            }
         });
         observer.observe(host);
         onCleanup(() => observer.disconnect());
      },
   );

   onCleanup(() => cancelAnimationFrame(frame_));

   function sizeSvg() {
      if (!host || !svg) {
         return;
      }
      svg.setAttribute("width", String(host.clientWidth));
      svg.setAttribute("height", String(host.clientHeight));
   }

   // One marker per kind: circle for a client, square for a profile, solid
   // diamond for the upstream, small red square for a rule. The shape carries
   // the kind so colour never has to.
   function markerFor(kind: NodeKind, x: number, y: number): SVGElement {
      const size = MARKER[kind];
      const half = size / 2;
      const stroke = INK;
      if (kind === "client" || kind === "upstream") {
         const fill = kind === "upstream" ? INK : "#ffffff";
         return el("circle", { cx: x, cy: y, r: half, fill, stroke, "stroke-width": 2 });
      }
      if (kind === "rule") {
         return el("rect", {
            x: x - half,
            y: y - half,
            width: size,
            height: size,
            fill: RED,
            stroke,
            "stroke-width": 1.5,
         });
      }
      return el("rect", {
         x: x - half,
         y: y - half,
         width: size,
         height: size,
         fill: "#ffffff",
         stroke,
         "stroke-width": 2,
      });
   }

   async function draw(topology: Topology) {
      if (!host || !svg) {
         return;
      }
      const measureLabel = measureWith(`600 12px ${fontStack}`);
      const measureDetail = measureWith(`400 10.5px ${fontStack}`);
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

      if (edgeLayer) {
         edgeLayer.replaceChildren(
            ...topology.edges.flatMap((edge) => {
               const from = placed.get(edge.from);
               const to = placed.get(edge.to);
               if (!from || !to) {
                  return [];
               }
               return [
                  el("line", {
                     x1: from.x,
                     y1: from.y,
                     x2: to.x,
                     y2: to.y,
                     stroke: INK_SOFT,
                     "stroke-width": 1,
                     "vector-effect": "non-scaling-stroke",
                  }),
               ];
            }),
         );
      }

      if (nodeLayer) {
         nodeLayer.replaceChildren(
            ...[...placed.values()].flatMap((node) => {
               const labelX = node.x + STAR_CELL / 2;
               const group = el("g", { "data-id": node.id });
               group.appendChild(markerFor(node.kind, node.x, node.y));
               const label = el("text", {
                  x: labelX,
                  y: node.y - 2,
                  fill: INK,
                  "font-family": fontStack,
                  "font-size": 12,
                  "font-weight": 650,
                  "letter-spacing": "0.06em",
               });
               label.textContent = node.label.toUpperCase();
               group.appendChild(label);
               const detail = el("text", {
                  x: labelX,
                  y: node.y + 13,
                  fill: "#5c5c58",
                  "font-family": fontStack,
                  "font-size": 10.5,
               });
               detail.textContent = node.detail;
               group.appendChild(detail);
               return [group];
            }),
         );
      }

      sizeSvg();
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
            camera = ensureVisible(camera, node, host.clientWidth, host.clientHeight, REVEAL_MARGIN);
            applyCamera();
         }
      }
   }

   // fitToView frames the whole graph, which is where a fresh page lands and
   // what the fit control asks for.
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

   function drawLinkRing() {
      const node = draft?.target ? placed.get(draft.target) : undefined;
      if (!linkRingBox) {
         return;
      }
      linkRingBox.style.display = node ? "" : "none";
      if (!node) {
         return;
      }
      const half = MARKER[node.kind] / 2 + 12;
      linkRingBox.setAttribute("x", String(node.x - half));
      linkRingBox.setAttribute("y", String(node.y - half));
      linkRingBox.setAttribute("width", String(half * 2));
      linkRingBox.setAttribute("height", String(half * 2));
      linkRingBox.setAttribute("stroke", draft?.legal ? GREEN : RED);
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

   // The effects layer is rebuilt per frame as one SVG string. A query burst is
   // capped at 120 particles (see flow.ts), so the churn stays small.
   //
   // An idle graph still ticks, but it must not touch the DOM. The layer used to
   // clear itself every frame regardless of what was drawn, which is a mutation
   // per frame while nothing happened. advance prunes dead particles, so skipping
   // it when both arrays are empty changes nothing it would have done, and
   // assigning innerHTML already replaces the previous frame's children.
   let lastTick = performance.now();
   let frame_ = requestAnimationFrame(function tick_(now: number) {
      const deltaSeconds = Math.min((now - lastTick) / 1000, 0.1);
      lastTick = now;
      if (fxLayer) {
         const parts: string[] = [];
         const moving = (flow.particles.length > 0 || flow.pulses.length > 0) && advance(flow, deltaSeconds);
         if (moving || draft) {
            for (const particle of flow.particles) {
               const ends = trail(particle);
               const color = hex(particle.color);
               parts.push(
                  `<line x1="${ends.from.x}" y1="${ends.from.y}" x2="${ends.to.x}" y2="${ends.to.y}" stroke="${color}" stroke-width="2" vector-effect="non-scaling-stroke"/>`,
               );
               parts.push(`<rect x="${ends.to.x - 2.5}" y="${ends.to.y - 2.5}" width="5" height="5" fill="${color}"/>`);
            }
            for (const pulse of flow.pulses) {
               const ratio = pulse.age / PULSE_LIFE_SECONDS;
               const half = 10 + ratio * 22;
               parts.push(
                  `<rect x="${pulse.x - half}" y="${pulse.y - half}" width="${half * 2}" height="${half * 2}" fill="none" stroke="${hex(pulse.color)}" stroke-width="1.5" opacity="${(1 - ratio).toFixed(2)}"/>`,
               );
            }
            if (draft) {
               const origin = placed.get(draft.from);
               if (origin) {
                  const color = draft.legal ? GREEN : RED;
                  parts.push(
                     `<line x1="${origin.x}" y1="${origin.y}" x2="${draft.x}" y2="${draft.y}" stroke="${color}" stroke-width="1.5" vector-effect="non-scaling-stroke"/>`,
                  );
                  parts.push(`<rect x="${draft.x - 3}" y="${draft.y - 3}" width="6" height="6" fill="${color}"/>`);
               }
            }
         }
         if (parts.length > 0) {
            fxLayer.innerHTML = parts.join("");
         } else if (fxLayer.childNodes.length > 0) {
            fxLayer.replaceChildren();
         }
      }
      frame_ = requestAnimationFrame(tick_);
   });

   function applyCamera() {
      if (!world) {
         return;
      }
      world.setAttribute("transform", `translate(${camera.x} ${camera.y}) scale(${camera.scale})`);
      setZoom(camera.scale);
   }

   // zoomBy steps the view about the viewport centre, which is what a keyboard
   // or a button wants: the wheel already handles zooming about the pointer.
   function zoomBy(factor: number) {
      if (!host) {
         return;
      }
      camera = zoomAt(camera, factor, host.clientWidth / 2, host.clientHeight / 2);
      applyCamera();
   }

   // ── Pointer controls ──────────────────────────────────────────────────

   function attachControls() {
      let gesture: Gesture = { kind: "idle" };
      let moved = 0;
      let last = { x: 0, y: 0 };
      const pointers = new Map<number, { x: number; y: number }>();

      const at = (event: PointerEvent | WheelEvent) => {
         const rect = svg!.getBoundingClientRect();
         return { x: event.clientX - rect.left, y: event.clientY - rect.top };
      };

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

      svg!.addEventListener("pointerdown", (event) => {
         const point = at(event);
         pointers.set(event.pointerId, point);
         moved = 0;
         last = point;
         if (pointers.size === 2) {
            const start = pinchState(camera.scale);
            if (start) {
               gesture = { kind: "pinch", ...start };
            }
            draft = undefined;
            return;
         }
         gesture = { kind: "press", node: pick(point.x, point.y) };
      });

      svg!.addEventListener("pointermove", (event) => {
         if (!pointers.has(event.pointerId)) {
            return;
         }
         const point = at(event);
         pointers.set(event.pointerId, point);
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
         const dx = point.x - last.x;
         const dy = point.y - last.y;
         moved += Math.abs(dx) + Math.abs(dy);
         last = point;
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
            updateDraft(gesture.from, point.x, point.y);
         }
      });

      const release = (event: PointerEvent) => {
         const point = pointers.get(event.pointerId) ?? at(event);
         pointers.delete(event.pointerId);
         if (gesture.kind === "pinch" && pointers.size < 2) {
            gesture = { kind: "pan" };
         }
         if (gesture.kind === "press") {
            if (moved < CLICK_TRAVEL) {
               setSelected(pick(point.x, point.y));
            }
            gesture = { kind: "idle" };
         } else if (gesture.kind === "link") {
            const target = pick(point.x, point.y);
            if (target && target !== gesture.from) {
               void applyLink(gesture.from, target);
            }
            draft = undefined;
            drawLinkRing();
            gesture = { kind: "idle" };
         }
         if (pointers.size === 0 && gesture.kind === "pan") {
            gesture = { kind: "idle" };
         }
      };

      svg!.addEventListener("pointerup", release);
      svg!.addEventListener("pointerupoutside", release as unknown as EventListener);
      svg!.addEventListener("pointerleave", (event) => release(event));

      svg!.addEventListener(
         "wheel",
         (event) => {
            event.preventDefault();
            const point = at(event);
            const factor = Math.pow(1.0015, -event.deltaY);
            camera = zoomAt(camera, factor, point.x, point.y);
            applyCamera();
         },
         { passive: false },
      );
   }

   createEffect(
      () => undefined,
      () => {
         if (!host || !svg) {
            return;
         }
         sizeSvg();
         attachControls();
         return () => undefined;
      },
   );

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
      drawLinkRing();
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
         void draw(buildTopology(props.profiles, props.clients, props.defaultProfile, props.upstreams, props.rules));
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
         <svg
            ref={(element) => {
               svg = element as unknown as SVGSVGElement;
            }}
         >
            <g
               ref={(element) => {
                  world = element as unknown as SVGGElement;
               }}
            >
               <g
                  ref={(element) => {
                     edgeLayer = element as unknown as SVGGElement;
                  }}
               />
               <g
                  ref={(element) => {
                     fxLayer = element as unknown as SVGGElement;
                  }}
               />
               <g
                  ref={(element) => {
                     nodeLayer = element as unknown as SVGGElement;
                  }}
               />
               <Show when={selectedNode()}>
                  {(node) => {
                     const half = MARKER[node().kind] / 2 + 8;
                     return (
                        <rect
                           x={node().x - half}
                           y={node().y - half}
                           width={half * 2}
                           height={half * 2}
                           fill="none"
                           stroke={RED}
                           stroke-width="2"
                        />
                     );
                  }}
               </Show>
               <rect
                  ref={(element) => {
                     linkRingBox = element as unknown as SVGRectElement;
                  }}
                  fill="none"
                  stroke-width="2"
                  style={{ display: "none" }}
               />
            </g>
         </svg>
         <span class="hint" aria-hidden="true">click a node · drag node to node to connect · scroll to zoom</span>
         <ul
            class="graph-legend"
            data-testid="graph-legend"
            style={{ position: "absolute", left: "18px", bottom: "54px", margin: 0, padding: 0, "list-style": "none", display: "flex", gap: "14px", "font-size": "11px", color: "var(--text-2)" }}
         >
            <li style={{ display: "flex", "align-items": "center", gap: "6px" }}>
               <span aria-hidden="true" style={{ width: "9px", height: "9px", background: GREEN, display: "inline-block" }} />
               allowed query
            </li>
            <li style={{ display: "flex", "align-items": "center", gap: "6px" }}>
               <span aria-hidden="true" style={{ width: "9px", height: "9px", background: RED, display: "inline-block" }} />
               blocked query
            </li>
            <li aria-hidden="true">marker size: client · profile · upstream · rule</li>
         </ul>
         <div class="graph-top">
            <form
               class="graph-create"
               onSubmit={(event) => {
                  event.preventDefault();
                  void createProfile();
               }}
            >
               <input
                  data-testid="graph-create-name"
                  aria-label="New profile name"
                  placeholder="new profile"
                  value={newProfile()}
                  onInput={(event) => setNewProfile(event.currentTarget.value)}
               />
               <button type="submit" class="btn-ghost" data-testid="graph-create" disabled={!newProfile().trim() || creating()}>
                  Add
               </button>
            </form>
            <div class="graph-controls">
               <button type="button" class="btn-ghost" data-testid="graph-zoom-out" aria-label="Zoom out" onClick={() => zoomBy(1 / 1.25)}>
                  −
               </button>
               <span class="graph-zoom" data-testid="graph-zoom">
                  {Math.round(zoom() * 100)}%
               </span>
               <button type="button" class="btn-ghost" data-testid="graph-zoom-in" aria-label="Zoom in" onClick={() => zoomBy(1.25)}>
                  +
               </button>
               <button type="button" class="btn-ghost" data-testid="graph-fit" onClick={() => fitToView()}>
                  Fit
               </button>
            </div>
         </div>
         <Show when={graphError()}>
            <p class="error graph-error" data-testid="graph-error">
               {graphError()}
            </p>
         </Show>
         <Show when={selectedClient()}>
            {(client) => (
               <div class="graph-panel" data-testid="graph-panel">
                  <header>
                     <h2>{client().name}</h2>
                     <button type="button" class="icon-btn" aria-label="close" onClick={() => setSelected(undefined)}>
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
                     <button type="button" class="icon-btn" aria-label="close" onClick={() => setSelected(undefined)}>
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
                     <button type="button" class="icon-btn" aria-label="close" onClick={() => setSelected(undefined)}>
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
            class="btn"
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
            class="btn"
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
