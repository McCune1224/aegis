import { defaultClientID, type NodeKind } from "./topology";

// A link is the policy change one drag expresses, resolved to a typed value
// before anything is written, so the renderer never decides what a gesture
// means and the panel sends it through the endpoint that owns the record.
export type LinkIntent =
  | { op: "reassign"; client: string; profile: string }
  | { op: "default"; profile: string }
  | { op: "extends"; child: string; parent: string };

export type LinkNode = { id: string; kind: NodeKind };

// LINKS names the pairs a drag may connect, directed from the dependent node to
// the dependency, which is also the direction the edges draw: client to
// profile, child profile to the parent it inherits from. Everything else,
// including the answer edges to upstreams, is derived configuration no drag
// should rewrite.
const LINKS: Record<string, true> = {
  "client>profile": true,
  "profile>profile": true,
};

function nameOf(id: string, kind: NodeKind): string {
  return id.slice(kind.length + 1);
}

export function linkIntent(nodes: LinkNode[], from: string, to: string): LinkIntent | undefined {
  const source = nodes.find((node) => node.id === from);
  const target = nodes.find((node) => node.id === to);
  if (!source || !target || from === to) {
    return undefined;
  }
  if (source.id === defaultClientID) {
    if (target.kind !== "profile") {
      return undefined;
    }
    return { op: "default", profile: nameOf(target.id, "profile") };
  }
  if (!LINKS[`${source.kind}>${target.kind}`]) {
    return undefined;
  }
  if (source.kind === "profile") {
    return { op: "extends", child: nameOf(source.id, "profile"), parent: nameOf(target.id, "profile") };
  }
  return { op: "reassign", client: nameOf(source.id, "client"), profile: nameOf(target.id, "profile") };
}
