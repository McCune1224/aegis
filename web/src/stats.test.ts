import { describe, expect, test } from "vitest";
import { aggregate, window24Hours, windowMinutes } from "./stats";

const NOW = Date.UTC(2026, 8, 15, 12, 0, 0);

function entry(minutesAgo: number, name: string, verdict: string, client: string) {
  return {
    time: new Date(NOW - minutesAgo * 60_000).toISOString(),
    client,
    name,
    type: "A",
    verdict,
  };
}

describe("aggregate", () => {
  test("counts verdicts and the block rate", () => {
    const stats = aggregate(
      [
        entry(0, "ads.example.com", "block", "10.9.9.2"),
        entry(1, "tracker.example.net", "block", "10.9.9.2"),
        entry(2, "ads.example.com", "block", "10.9.9.3"),
        entry(3, "example.org", "allow", "10.9.9.2"),
      ],
      NOW,
    );
    expect(stats.total).toBe(4);
    expect(stats.blocked).toBe(3);
    expect(stats.allowed).toBe(1);
    expect(stats.blockRate).toBeCloseTo(0.75);
    expect(stats.clients).toBe(2);
  });

  test("lands entries in one minute buckets over the last hour", () => {
    const stats = aggregate(
      [
        entry(0, "fresh.example.com", "block", "10.9.9.2"),
        entry(0, "fresh.example.com", "block", "10.9.9.2"),
        entry(59, "stale.example.com", "allow", "10.9.9.3"),
        entry(120, "ancient.example.com", "block", "10.9.9.3"),
      ],
      NOW,
    );
    expect(stats.series).toHaveLength(windowMinutes);
    expect(stats.series[windowMinutes - 1]).toEqual({ t: NOW, total: 2, blocked: 2 });
    expect(stats.series[0]).toEqual({ t: NOW - 59 * 60_000, total: 1, blocked: 0 });
    expect(stats.blocked).toBe(3);
    expect(stats.total).toBe(4);
  });

  test("ranks the top blocked names and top clients", () => {
    const stats = aggregate(
      [
        entry(0, "ads.example.com", "block", "10.9.9.2"),
        entry(1, "ads.example.com", "block", "10.9.9.2"),
        entry(2, "ads.example.com", "block", "10.9.9.3"),
        entry(3, "tracker.example.net", "block", "10.9.9.3"),
        entry(4, "example.org", "allow", "10.9.9.3"),
      ],
      NOW,
    );
    expect(stats.topBlocked).toEqual([
      { name: "ads.example.com", count: 3 },
      { name: "tracker.example.net", count: 1 },
    ]);
    expect(stats.topClients).toEqual([
      { client: "10.9.9.3", count: 3 },
      { client: "10.9.9.2", count: 2 },
    ]);
  });

  test("reports zeros over an empty log", () => {
    const stats = aggregate([], NOW);
    expect(stats.total).toBe(0);
    expect(stats.blockRate).toBe(0);
    expect(stats.clients).toBe(0);
    expect(stats.topBlocked).toEqual([]);
    expect(stats.series).toHaveLength(windowMinutes);
  });
});

describe("aggregate windows", () => {
  test("the 24 hour window spreads the same entries across half hour buckets", () => {
    const now = Date.UTC(2026, 8, 15, 12, 0, 0);
    const hoursAgo = (hours: number) => new Date(now - hours * 3_600_000).toISOString();
    const entries = [
      { time: hoursAgo(23), client: "10.9.9.2", name: "old.example.com", type: "A", verdict: "allow" },
      { time: hoursAgo(1), client: "10.9.9.2", name: "new.example.com", type: "A", verdict: "block" },
    ];
    const stats = aggregate(entries, now, window24Hours);
    expect(stats.series).toHaveLength(60);
    expect(stats.series[0].total).toBe(0);
    expect(stats.series[1]).toEqual({ t: now - 1392 * 60_000, total: 1, blocked: 0 });
    expect(stats.series[56]).toEqual({ t: now - 72 * 60_000, total: 1, blocked: 1 });
  });
});
