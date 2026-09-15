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

export const upstreamID = "upstream";
export const defaultClientID = "client:unidentified";

// buildTopology is the shape the resolver actually applies: clients point at a
// profile, profiles inherit from a parent, and every profile answers through the
// upstream. The same structure the engine compiles is what the canvas draws.
export function buildTopology(
  profiles: Profile[],
  clients: Client[],
  defaultProfile: string,
  upstream: string,
): Topology {
  const nodes: GraphNode[] = [];
  const edges: GraphEdge[] = [];

  nodes.push({ id: upstreamID, label: "upstream", detail: upstream || "not configured", kind: "upstream" });

  for (const profile of profiles) {
    nodes.push({
      id: `profile:${profile.name}`,
      label: profile.name,
      detail: profile.mode ?? "inherited",
      kind: "profile",
    });
    edges.push({ id: `answer:${profile.name}`, from: `profile:${profile.name}`, to: upstreamID });
  }

  for (const profile of profiles) {
    if (profile.extends) {
      edges.push({ id: `extends:${profile.name}`, from: `profile:${profile.name}`, to: `profile:${profile.extends}` });
    }
  }

  if (defaultProfile) {
    nodes.push({ id: defaultClientID, label: "unidentified", detail: `default ${defaultProfile}`, kind: "client" });
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
