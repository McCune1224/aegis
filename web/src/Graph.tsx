import ELK, { type ElkNode } from "elkjs/lib/elk.bundled.js";
import { Application, Container, Graphics, Text } from "pixi.js";
import { createEffect } from "solid-js";
import type { Client, Profile } from "./api";
import { buildTopology, type Topology } from "./topology";

type Props = {
  profiles: Profile[];
  clients: Client[];
  defaultProfile: string;
  upstream: string;
};

const nodeWidth = 176;
const nodeHeight = 56;
const background = 0x101418;
const border = 0x30363d;

const kindColors: Record<string, number> = {
  client: 0x1f6feb,
  profile: 0x2ea043,
  upstream: 0x8957e5,
};

const elk = new ELK();

export default function Graph(props: Props) {
  let host: HTMLDivElement | undefined;
  let app: Application | undefined;

  createEffect(
    () => buildTopology(props.profiles, props.clients, props.defaultProfile, props.upstream),
    (topology) => {
      void draw(topology);
    },
  );

  async function ensureApp(): Promise<Application> {
    if (app) {
      return app;
    }
    const instance = new Application();
    await instance.init({
      background,
      resizeTo: host,
      antialias: true,
      preserveDrawingBuffer: true,
    });
    host?.appendChild(instance.canvas);
    app = instance;
    return instance;
  }

  async function draw(topology: Topology) {
    const instance = await ensureApp();
    const layout = await elk.layout({
      id: "root",
      layoutOptions: {
        "elk.algorithm": "layered",
        "elk.direction": "RIGHT",
        "elk.spacing.nodeNode": "36",
        "elk.layered.spacing.nodeNodeBetweenLayers": "96",
      },
      children: topology.nodes.map((node) => ({ id: node.id, width: nodeWidth, height: nodeHeight })),
      edges: topology.edges.map((edge) => ({ id: edge.id, sources: [edge.from], targets: [edge.to] })),
    });

    const placed = new Map<string, ElkNode>();
    for (const child of layout.children ?? []) {
      placed.set(child.id, child);
    }

    const stage = instance.stage;
    stage.removeChildren();

    const lines = new Graphics();
    for (const edge of topology.edges) {
      const from = placed.get(edge.from);
      const to = placed.get(edge.to);
      if (!from || !to) {
        continue;
      }
      lines
        .moveTo((from.x ?? 0) + nodeWidth, (from.y ?? 0) + nodeHeight / 2)
        .lineTo(to.x ?? 0, (to.y ?? 0) + nodeHeight / 2)
        .stroke({ width: 1.5, color: border });
    }
    stage.addChild(lines);

    const boxes = new Container();
    for (const node of topology.nodes) {
      const place = placed.get(node.id);
      if (!place) {
        continue;
      }
      const x = place.x ?? 0;
      const y = place.y ?? 0;

      const box = new Graphics();
      box
        .roundRect(x, y, nodeWidth, nodeHeight, 10)
        .fill(0x161b22)
        .stroke({ width: 1.5, color: kindColors[node.kind] ?? border });

      const label = new Text({ text: node.label, style: { fill: 0xe6edf3, fontSize: 14, fontWeight: "600" } });
      label.x = x + 12;
      label.y = y + 8;

      const detail = new Text({ text: node.detail, style: { fill: 0x9da7b3, fontSize: 11 } });
      detail.x = x + 12;
      detail.y = y + 28;

      boxes.addChild(box, label, detail);
    }
    stage.addChild(boxes);
  }

  return (
    <div
      class="graph"
      data-testid="graph"
      ref={(element) => {
        host = element as HTMLDivElement;
      }}
    />
  );
}
