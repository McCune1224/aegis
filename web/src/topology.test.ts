import { describe, expect, it } from "vitest";
import type { Client } from "./api";
import { clientFor, defaultClientID } from "./topology";

const phone: Client = {
  name: "phone",
  profile: "kids",
  notes: "",
  addresses: ["10.9.9.44"],
  macs: ["aa:bb:cc:dd:ee:01"],
  prefixes: [],
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
