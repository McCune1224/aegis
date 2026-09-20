import { describe, expect, it } from "vitest";
import { suggestedName } from "./Discoveries";
import type { Discovery } from "./api";

function discovery(overrides: Partial<Discovery> = {}): Discovery {
  return { mac: "aa:bb:cc:dd:ee:09", address: "10.9.9.30", first: 0, last: 0, ...overrides };
}

describe("suggestedName", () => {
  it("uses the hostname the device gave the DHCP server", () => {
    expect(suggestedName(discovery({ hostname: "phone" }))).toBe("phone");
  });

  it("falls back to the hardware address so the field is never empty", () => {
    expect(suggestedName(discovery())).toBe("aabbccddee09");
  });

  it("ignores a hostname that is only spaces", () => {
    expect(suggestedName(discovery({ hostname: "   " }))).toBe("aabbccddee09");
  });
});
