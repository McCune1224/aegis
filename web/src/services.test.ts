import { describe, expect, test } from "vitest";
import type { BlockedService } from "./api";
import { groupServices, serviceGroupLabel, groupState, toggled, toggledGroup } from "./services";

const youtube: BlockedService = {
  id: "youtube",
  name: "YouTube",
  group: "streaming",
  rule_count: 2,
  profiles: [],
  clients: [],
};

const fourchan: BlockedService = {
  id: "4chan",
  name: "4chan",
  group: "social_network",
  rule_count: 1,
  profiles: [],
  clients: [],
};

const orphan: BlockedService = {
  id: "orphan",
  name: "Orphan",
  group: "",
  rule_count: 1,
  profiles: [],
  clients: [],
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

describe("groupState", () => {
  const group = ["youtube", "4chan"];

  test("is none when no service in the group is selected", () => {
    expect(groupState(group, [])).toBe("none");
    expect(groupState(group, ["netflix"])).toBe("none");
  });

  test("is some when part of the group is selected", () => {
    expect(groupState(group, ["youtube"])).toBe("some");
  });

  test("is all when every service in the group is selected", () => {
    expect(groupState(group, ["youtube", "4chan"])).toBe("all");
    expect(groupState(group, ["netflix", "youtube", "4chan"])).toBe("all");
  });
});

describe("toggledGroup", () => {
  const group = ["youtube", "4chan"];

  test("adds every service when the group is not fully selected", () => {
    expect(toggledGroup(["youtube"], group)).toEqual(["youtube", "4chan"]);
  });

  test("removes every service when the group is fully selected", () => {
    expect(toggledGroup(["youtube", "4chan", "netflix"], group)).toEqual(["netflix"]);
  });

  test("does not duplicate a service already selected", () => {
    expect(toggledGroup(["youtube"], ["youtube"])).toEqual([]);
    expect(toggledGroup(["youtube"], ["youtube", "netflix"])).toEqual(["youtube", "netflix"]);
  });
});

describe("serviceGroupLabel", () => {
  test("names the known catalog groups in plain words", () => {
    expect(serviceGroupLabel("ai")).toBe("Artificial intelligence");
    expect(serviceGroupLabel("social_network")).toBe("Social networks");
    expect(serviceGroupLabel("gambling")).toBe("Gambling and betting");
  });

  test("renders an unknown group id readably instead of raw", () => {
    expect(serviceGroupLabel("cloud_gaming")).toBe("Cloud Gaming");
    expect(serviceGroupLabel("crypto")).toBe("Crypto");
  });

  test("the empty group is Other", () => {
    expect(serviceGroupLabel("")).toBe("Other");
  });
});
