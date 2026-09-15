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

// matchClient resolves a query's source address to a client node id, the way
// the identity resolver does: exact address first, then the first prefix that
// contains it. Undefined means the query came from an unknown device.
export function matchClient(clients: Client[], address: string): string | undefined {
  for (const client of clients) {
    if (client.addresses.some((candidate) => candidate === address)) {
      return `client:${client.name}`;
    }
  }
  for (const client of clients) {
    if (client.prefixes.some((prefix) => withinPrefix(address, prefix))) {
      return `client:${client.name}`;
    }
  }
  return undefined;
}

function withinPrefix(address: string, prefix: string): boolean {
  const [base, bitsText] = prefix.split("/");
  const bits = Number(bitsText);
  if (!base || !Number.isFinite(bits) || bits < 0) {
    return false;
  }
  const addressBytes = parseBytes(address);
  const baseBytes = parseBytes(base);
  if (!addressBytes || !baseBytes || addressBytes.length !== baseBytes.length) {
    return false;
  }
  const whole = Math.floor(bits / 8);
  if (whole > addressBytes.length) {
    return false;
  }
  for (let index = 0; index < whole; index++) {
    if (addressBytes[index] !== baseBytes[index]) {
      return false;
    }
  }
  const remainder = bits % 8;
  if (remainder === 0 || whole >= addressBytes.length) {
    return true;
  }
  const mask = 0xff << (8 - remainder);
  return (addressBytes[whole] & mask) === (baseBytes[whole] & mask);
}

function parseBytes(text: string): number[] | undefined {
  if (text.includes(":")) {
    const hextets = expandIpv6(text);
    return hextets ? hextetsToBytes(hextets) : undefined;
  }
  const parts = text.split(".").map(Number);
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) {
    return undefined;
  }
  return parts;
}

function expandIpv6(text: string): string[] | undefined {
  const halves = text.split("::");
  if (halves.length > 2) {
    return undefined;
  }
  const groups = (half: string) => (half === "" ? [] : half.split(":"));
  const head = groups(halves[0]);
  const tail = halves.length === 2 ? groups(halves[1]) : [];
  const missing = 8 - head.length - tail.length;
  if (missing < 0 || (halves.length === 1 && missing !== 0)) {
    return undefined;
  }
  const all = [...head, ...Array(missing).fill("0"), ...tail];
  if (all.length !== 8) {
    return undefined;
  }
  return all;
}

function hextetsToBytes(hextets: string[]): number[] | undefined {
  const bytes: number[] = [];
  for (const hextet of hextets) {
    const value = parseInt(hextet, 16);
    if (!Number.isInteger(value) || value < 0 || value > 0xffff) {
      return undefined;
    }
    bytes.push(value >> 8, value & 0xff);
  }
  return bytes;
}
