import { describe, expect, it } from "vitest";
import { linkIntent, type LinkNode } from "./edit";
import { defaultClientID } from "./topology";

const nodes: LinkNode[] = [
  { id: "client:phone", kind: "client" },
  { id: defaultClientID, kind: "client" },
  { id: "profile:kids", kind: "profile" },
  { id: "profile:strict", kind: "profile" },
  { id: "upstream:127.0.0.1:5354", kind: "upstream" },
  { id: "rule:7", kind: "rule" },
];

describe("linkIntent", () => {
  it("reassigns a client when dragged onto another profile", () => {
    expect(linkIntent(nodes, "client:phone", "profile:kids")).toEqual({
      op: "reassign",
      client: "phone",
      profile: "kids",
    });
  });

  it("sets the default policy when the unidentified node is dragged onto a profile", () => {
    expect(linkIntent(nodes, defaultClientID, "profile:strict")).toEqual({
      op: "default",
      profile: "strict",
    });
  });

  it("extends a profile when dragged onto the parent it should inherit from", () => {
    expect(linkIntent(nodes, "profile:kids", "profile:strict")).toEqual({
      op: "extends",
      child: "kids",
      parent: "strict",
    });
  });

  it("refuses a profile dropped on itself", () => {
    expect(linkIntent(nodes, "profile:kids", "profile:kids")).toBeUndefined();
  });

  it("refuses pairs no endpoint owns, in either direction", () => {
    expect(linkIntent(nodes, "client:phone", "client:phone")).toBeUndefined();
    expect(linkIntent(nodes, "profile:kids", "client:phone")).toBeUndefined();
    expect(linkIntent(nodes, "profile:kids", "upstream:127.0.0.1:5354")).toBeUndefined();
    expect(linkIntent(nodes, "upstream:127.0.0.1:5354", "profile:kids")).toBeUndefined();
    expect(linkIntent(nodes, "client:phone", "upstream:127.0.0.1:5354")).toBeUndefined();
    expect(linkIntent(nodes, "rule:7", "profile:kids")).toBeUndefined();
    expect(linkIntent(nodes, "rule:7", "client:phone")).toBeUndefined();
  });

  it("refuses ids nothing holds", () => {
    expect(linkIntent(nodes, "client:ghost", "profile:kids")).toBeUndefined();
    expect(linkIntent(nodes, "client:phone", "profile:ghost")).toBeUndefined();
  });
});
