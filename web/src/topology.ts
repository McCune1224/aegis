import type { Client, Profile } from "./api";

export type NodeKind = "client" | "profile" | "upstream";

export type GraphNode = {
  id: string;
  label: string;
  detail: string;
  kind: NodeKind;
};

export type GraphEdge = {
  id: string;
  from: string;
  to: string;
};

export type Topology = {
  nodes: GraphNode[];
  edges: GraphEdge[];
};

export function upstreamNodeID(address: string): string {
  return `upstream:${address}`;
}
export const defaultClientID = "client:unidentified";

// buildTopology is the shape the resolver actually applies: clients point at a
// profile, profiles inherit from a parent, and every profile answers through
// every configured upstream, because the pool fails over between them. The
// same structure the engine compiles is what the canvas draws.
export function buildTopology(
  profiles: Profile[],
  clients: Client[],
  defaultProfile: string,
  upstreams: string[],
): Topology {
  const nodes: GraphNode[] = [];
  const edges: GraphEdge[] = [];

  for (const address of upstreams) {
    nodes.push({ id: upstreamNodeID(address), label: "upstream", detail: address, kind: "upstream" });
  }

  for (const profile of profiles) {
    nodes.push({
      id: `profile:${profile.name}`,
      label: profile.name,
      detail: profile.mode ?? "inherited",
      kind: "profile",
    });
    for (const address of upstreams) {
      edges.push({
        id: `answer:${profile.name}:${address}`,
        from: `profile:${profile.name}`,
        to: upstreamNodeID(address),
      });
    }
  }

  for (const profile of profiles) {
    if (profile.extends) {
      edges.push({ id: `extends:${profile.name}`, from: `profile:${profile.name}`, to: `profile:${profile.extends}` });
    }
  }

  if (defaultProfile) {
    nodes.push({ id: defaultClientID, label: "unidentified", detail: "default policy", kind: "client" });
    edges.push({ id: "policy:unidentified", from: defaultClientID, to: `profile:${defaultProfile}` });
  }

  for (const client of clients) {
    nodes.push({
      id: `client:${client.name}`,
      label: client.name,
      detail: [...client.addresses, ...client.prefixes].join(", ") || "no selectors",
      kind: "client",
    });
    edges.push({ id: `policy:${client.name}`, from: `client:${client.name}`, to: `profile:${client.profile}` });
  }

  return { nodes, edges };
}

// clientFor maps a decision's client key to the nodes the graph draws between,
// the way the resolver named it. The key comes from the server, so the graph
// never resolves an address itself, which cannot see a hardware address or a
// lease. A key nothing claims is the unidentified node, and an empty default
// profile leaves nothing to draw.
export function clientFor(
  key: string,
  clients: Client[],
  defaultProfile: string,
): { clientID: string; profileID: string } | undefined {
  const client = clients.find((candidate) => candidate.name === key);
  if (client) {
    return { clientID: `client:${client.name}`, profileID: `profile:${client.profile}` };
  }
  if (defaultProfile) {
    return { clientID: defaultClientID, profileID: `profile:${defaultProfile}` };
  }
  return undefined;
}
