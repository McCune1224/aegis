import type { Client, Discovery, Observed, Profile, Rule } from "./api";

export type NodeKind = "client" | "profile" | "upstream" | "rule" | "device";

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

// Device is one hardware address the server has seen that no client record
// names: a DHCP discovery, a query-log address, or both at once. It holds no
// policy, so the graph draws it passively and the panel is how it becomes a
// client.
export type Device = {
  id: string;
  label: string;
  detail: string;
  address: string;
  mac?: string;
  source: "dhcp" | "queries" | "dhcp + queries";
};

// seenDevices merges the two sighting feeds into one passive-device list,
// dropping anything a saved client already claims. One address seen by both
// feeds is one device; the hardware address and the hostname survive the merge.
export function seenDevices(
  discoveries: Discovery[],
  observed: Observed[],
  clients: Client[],
): Device[] {
  const claimed = new Set(clients.flatMap((client) => client.addresses));
  const queried = new Map<string, Observed>();
  for (const entry of observed) {
    if (!entry.claimed && !claimed.has(entry.client)) {
      queried.set(entry.client, entry);
    }
  }

  const devices: Device[] = [];
  for (const discovery of discoveries) {
    if (claimed.has(discovery.address)) {
      continue;
    }
    const source: Device["source"] = queried.has(discovery.address) ? "dhcp + queries" : "dhcp";
    devices.push({
      id: `device:${discovery.address}`,
      label: discovery.hostname || discovery.address,
      detail: `${discovery.address} · ${source}`,
      address: discovery.address,
      mac: discovery.mac,
      source,
    });
    queried.delete(discovery.address);
  }
  for (const entry of queried.values()) {
    devices.push({
      id: `device:${entry.client}`,
      label: entry.client,
      detail: `${entry.client} · queries`,
      address: entry.client,
      source: "queries",
    });
  }
  return devices;
}

// buildTopology is the shape the resolver actually applies: clients point at a
// profile, profiles inherit from a parent, and every profile answers through
// every configured upstream, because the pool fails over between them. The
// same structure the engine compiles is what the canvas draws.
export function buildTopology(
  profiles: Profile[],
  clients: Client[],
  defaultProfile: string,
  upstreams: string[],
  rules: Rule[] = [],
  devices: Device[] = [],
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

  for (const device of devices) {
    nodes.push({ id: device.id, label: device.label, detail: device.detail, kind: "device" });
  }

  for (const rule of rules) {
    nodes.push({
      id: `rule:${rule.id}`,
      label: rule.domain,
      detail: [rule.action, rule.kind, rule.client].filter(Boolean).join(" · "),
      kind: "rule",
    });
    if (rule.client && clients.some((candidate) => candidate.name === rule.client)) {
      edges.push({ id: `scope:${rule.id}`, from: `rule:${rule.id}`, to: `client:${rule.client}` });
    }
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
