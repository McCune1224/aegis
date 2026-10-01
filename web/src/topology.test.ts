import { describe, expect, it } from "vitest";
import type { Client, Discovery, Observed, Profile, Rule } from "./api";
import { buildTopology, clientFor, defaultClientID, seenDevices } from "./topology";

const phone: Client = {
  name: "phone",
  profile: "kids",
  notes: "",
  addresses: ["10.9.9.44"],
  macs: ["aa:bb:cc:dd:ee:01"],
  prefixes: [],
};

const kids: Profile = { name: "kids", mode: "nxdomain" };

const ads: Rule = { id: 7, domain: "ads.example.com", kind: "subdomains", action: "block" };

const games: Rule = {
  id: 9,
  domain: "games.example.com",
  kind: "exact",
  action: "allow",
  schedule: "evenings",
  client: "phone",
};

describe("clientFor", () => {
  it("draws the client the server resolved, whatever identified it", () => {
    expect(clientFor("phone", [phone], "default")).toEqual({
      clientID: "client:phone",
      profileID: "profile:kids",
    });
  });

  it("draws the unidentified node for a key nothing claims", () => {
    expect(clientFor("", [phone], "default")).toEqual({
      clientID: defaultClientID,
      profileID: "profile:default",
    });
  });

  it("draws the unidentified node for a key no client record holds", () => {
    expect(clientFor("ghost", [phone], "default")).toEqual({
      clientID: defaultClientID,
      profileID: "profile:default",
    });
  });

  it("draws nothing when there is no default profile to fall back to", () => {
    expect(clientFor("ghost", [phone], "")).toBeUndefined();
  });
});

describe("buildTopology with rules", () => {
  it("places one node per custom rule, naming what it does", () => {
    const topology = buildTopology([kids], [phone], "default", [], [ads, games]);
    const ruleNodes = topology.nodes.filter((node) => node.kind === "rule");
    expect(ruleNodes).toEqual([
      { id: "rule:7", label: "ads.example.com", detail: "block · subdomains", kind: "rule" },
      { id: "rule:9", label: "games.example.com", detail: "allow · exact · phone", kind: "rule" },
    ]);
  });

  it("wires a rule scoped to a client to that client's node", () => {
    const topology = buildTopology([kids], [phone], "default", [], [games]);
    expect(topology.edges).toContainEqual({ id: "scope:9", from: "rule:9", to: "client:phone" });
  });

  it("leaves an unscoped rule unconnected", () => {
    const topology = buildTopology([kids], [phone], "default", [], [ads]);
    expect(topology.edges.filter((edge) => edge.id.startsWith("scope:"))).toEqual([]);
  });

  it("draws no edge for a scope whose client node is absent", () => {
    const topology = buildTopology([kids], [], "default", [], [games]);
    expect(topology.edges.filter((edge) => edge.id.startsWith("scope:"))).toEqual([]);
  });

  it("places no rule nodes when no custom rules exist", () => {
    const topology = buildTopology([kids], [phone], "default", [], []);
    expect(topology.nodes.filter((node) => node.kind === "rule")).toEqual([]);
  });
});

describe("seen devices the graph draws passively", () => {
  const discovery: Discovery = {
    mac: "aa:bb:cc:00:00:42",
    address: "10.9.9.77",
    hostname: "livingroom-tv",
    first: 1,
    last: 2,
  };
  const observed: Observed = { client: "127.0.0.1", queries: 4, last_seen: 2, claimed: false };

  it("turns an unclaimed discovery into a node named for its hostname", () => {
    expect(seenDevices([discovery], [], [])).toEqual([
      { id: "device:10.9.9.77", label: "livingroom-tv", detail: "10.9.9.77 · dhcp", address: "10.9.9.77", mac: "aa:bb:cc:00:00:42", source: "dhcp" },
    ]);
  });

  it("names a discovery with no hostname for its address", () => {
    const unnamed: Discovery = { ...discovery, hostname: "" };
    expect(seenDevices([unnamed], [], [])[0].label).toBe("10.9.9.77");
  });

  it("draws an unclaimed observed address as a query-seen device", () => {
    expect(seenDevices([], [observed], [])).toEqual([
      { id: "device:127.0.0.1", label: "127.0.0.1", detail: "127.0.0.1 · queries", address: "127.0.0.1", mac: undefined, source: "queries" },
    ]);
  });

  it("merges one address seen twice into one device and says both", () => {
    const sameAddress: Observed = { client: "10.9.9.77", queries: 9, last_seen: 5, claimed: false };
    const devices = seenDevices([discovery], [sameAddress], []);
    expect(devices).toHaveLength(1);
    expect(devices[0].detail).toBe("10.9.9.77 · dhcp + queries");
    expect(devices[0].mac).toBe("aa:bb:cc:00:00:42");
  });

  it("skips a claimed observed address and an address a client record holds", () => {
    const claimed: Observed = { ...observed, claimed: true };
    expect(seenDevices([], [claimed], [])).toEqual([]);
    const taken: Client = { ...phone, addresses: ["10.9.9.77"] };
    expect(seenDevices([discovery], [], [taken])).toEqual([]);
  });
});

describe("buildTopology with seen devices", () => {
  it("places devices as passive nodes of their own kind, wired to nothing", () => {
    const devices = seenDevices(
      [{ mac: "aa:bb:cc:00:00:42", address: "10.9.9.77", hostname: "livingroom-tv", first: 1, last: 2 }],
      [{ client: "127.0.0.1", queries: 4, last_seen: 2, claimed: false }],
      [],
    );
    const topology = buildTopology([kids], [phone], "default", [], [], devices);
    expect(topology.nodes.filter((node) => node.kind === "device")).toEqual([
      { id: "device:10.9.9.77", label: "livingroom-tv", detail: "10.9.9.77 · dhcp", kind: "device" },
      { id: "device:127.0.0.1", label: "127.0.0.1", detail: "127.0.0.1 · queries", kind: "device" },
    ]);
    expect(topology.edges.filter((edge) => edge.id.includes("device:"))).toEqual([]);
  });
});
