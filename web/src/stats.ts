import type { QueryEntry } from "./api";

export type Bucket = { t: number; total: number; blocked: number };

export type Stats = {
  total: number;
  blocked: number;
  allowed: number;
  blockRate: number;
  clients: number;
  series: Bucket[];
  topBlocked: { name: string; count: number }[];
  topClients: { client: string; count: number }[];
};

export const windowMinutes = 60;

// aggregate turns a recent query log slice into the dashboard's numbers: one
// bucket per minute over the last hour, plus the top lists. now is passed in so
// the result only depends on the entries and the clock handed to it.
export function aggregate(entries: QueryEntry[], now: number): Stats {
  const buckets = windowMinutes;
  const bucketMs = 60_000;
  const windowStart = now - (buckets - 1) * bucketMs;
  const series: Bucket[] = Array.from({ length: buckets }, (_, index) => ({
    t: windowStart + index * bucketMs,
    total: 0,
    blocked: 0,
  }));

  const blockedNames = new Map<string, number>();
  const clientCounts = new Map<string, number>();
  const clients = new Set<string>();
  let blocked = 0;

  for (const entry of entries) {
    const time = Date.parse(entry.time);
    if (Number.isNaN(time)) {
      continue;
    }
    const index = Math.floor((time - windowStart) / bucketMs);
    if (index >= 0 && index < buckets) {
      series[index].total += 1;
      if (entry.verdict === "block") {
        series[index].blocked += 1;
      }
    }

    if (entry.verdict === "block") {
      blocked += 1;
      blockedNames.set(entry.name, (blockedNames.get(entry.name) ?? 0) + 1);
    }
    clientCounts.set(entry.client, (clientCounts.get(entry.client) ?? 0) + 1);
    clients.add(entry.client);
  }

  return {
    total: entries.length,
    blocked,
    allowed: entries.length - blocked,
    blockRate: entries.length === 0 ? 0 : blocked / entries.length,
    clients: clients.size,
    series,
    topBlocked: top(blockedNames),
    topClients: top(clientCounts).map((row) => ({ client: row.name, count: row.count })),
  };
}

function top(counts: Map<string, number>, limit = 8): { name: string; count: number }[] {
  return [...counts.entries()]
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, limit);
}
