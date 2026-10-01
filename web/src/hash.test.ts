import { describe, expect, it } from "vitest";
import { appHashFor, parseAppHash } from "./hash";

describe("parseAppHash", () => {
  it("reads the tab and the system sub view", () => {
    expect(parseAppHash("#/clients")).toEqual({ tab: "clients", system: "upstreams" });
    expect(parseAppHash("#/system/sources")).toEqual({ tab: "system", system: "sources" });
  });

  it("answers the home tab for bare and unknown hashes", () => {
    expect(parseAppHash("")).toEqual({ tab: "dashboard", system: "upstreams" });
    expect(parseAppHash("#/nope")).toEqual({ tab: "dashboard", system: "upstreams" });
    expect(parseAppHash("#/system/nope")).toEqual({ tab: "system", system: "upstreams" });
  });

  it("lands an old schedules link on the services page that now owns them", () => {
    expect(parseAppHash("#/schedules")).toEqual({ tab: "services", system: "upstreams" });
  });
});

describe("appHashFor", () => {
  it("serializes the home tab as the bare root", () => {
    expect(appHashFor("dashboard", "upstreams")).toBe("#/");
    expect(appHashFor("log", "upstreams")).toBe("#/log");
    expect(appHashFor("system", "sources")).toBe("#/system/sources");
  });
});
