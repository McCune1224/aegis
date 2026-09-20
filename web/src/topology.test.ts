import { describe, expect, it } from "vitest";
import type { Client, Profile, Rule } from "./api";
import { buildTopology, clientFor, defaultClientID } from "./topology";

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
