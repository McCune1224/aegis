import { describe, expect, test } from "vitest";
import { formatLatency, health } from "./Upstreams";

describe("formatLatency", () => {
  test("an unmeasured upstream says so", () => {
    expect(formatLatency(0)).toBe("unmeasured");
  });

  test("sub-millisecond latencies keep one decimal", () => {
    expect(formatLatency(3.42)).toBe("3.4 ms");
  });

  test("whole milliseconds round away the float noise", () => {
    expect(formatLatency(212.7)).toBe("213 ms");
  });
});

describe("health", () => {
  const base = {
    name: "quadrant",
    url: "8.8.8.8:53",
    enabled: true,
    backup: false,
    latency_ms: 12,
    failures: 0,
    down: false,
  };

  test("a down upstream blocks", () => {
    expect(health({ ...base, down: true })).toEqual({ label: "down", kind: "block" });
  });

  test("a failing upstream shows its streak", () => {
    expect(health({ ...base, failures: 3 })).toEqual({ label: "failing ×3", kind: "block" });
  });

  test("a disabled upstream is neither ok nor failing", () => {
    expect(health({ ...base, enabled: false })).toEqual({ label: "disabled", kind: "kind" });
  });

  test("a serving upstream is ok", () => {
    expect(health(base)).toEqual({ label: "ok", kind: "allow" });
  });
});
