import { describe, expect, test } from "vitest";
import type { BlockedService } from "./api";
import { groupServices, toggled } from "./services";

const youtube: BlockedService = {
  id: "youtube",
  name: "YouTube",
  group: "streaming",
  rule_count: 2,
  profiles: [],
};

const fourchan: BlockedService = {
  id: "4chan",
  name: "4chan",
  group: "social_network",
  rule_count: 1,
  profiles: [],
};

const orphan: BlockedService = {
  id: "orphan",
  name: "Orphan",
  group: "",
  rule_count: 1,
  profiles: [],
};

describe("groupServices", () => {
  test("orders groups the way the catalog lists them", () => {
    expect(groupServices([youtube, fourchan], ["streaming", "social_network"])).toEqual([
      { group: "streaming", services: [youtube] },
      { group: "social_network", services: [fourchan] },
    ]);
  });

  test("puts services that name no group last", () => {
    expect(groupServices([orphan, youtube], ["streaming"])).toEqual([
      { group: "streaming", services: [youtube] },
      { group: "", services: [orphan] },
    ]);
  });

  test("keeps a group the groups list omits", () => {
    expect(groupServices([fourchan], [])).toEqual([
      { group: "social_network", services: [fourchan] },
    ]);
  });

  test("drops a group the catalog has no service for", () => {
    expect(groupServices([youtube], ["streaming", "gambling"])).toEqual([
      { group: "streaming", services: [youtube] },
    ]);
  });
});

describe("toggled", () => {
  test("adds a service that is off", () => {
    expect(toggled([], "youtube")).toEqual(["youtube"]);
  });

  test("removes a service that is on", () => {
    expect(toggled(["youtube", "4chan"], "youtube")).toEqual(["4chan"]);
  });

  test("keeps the services already on when adding", () => {
    expect(toggled(["4chan"], "youtube")).toEqual(["4chan", "youtube"]);
  });
});
