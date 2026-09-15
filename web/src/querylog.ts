import { createSignal } from "solid-js";
import { listQueries, streamQueries, type Decision, type QueryEntry, type QueryFilterInput } from "./api";

export type QueryLog = {
  entries: () => QueryEntry[];
  live: () => boolean;
  setLive: (live: boolean) => void;
  load: (filter: QueryFilterInput) => Promise<void>;
  subscribe: (onEvent: (decision: Decision) => void) => () => void;
};

const cap = 5000;

export function createQueryLog(options: { live?: boolean } = {}): QueryLog {
  const [entries, setEntries] = createSignal<QueryEntry[]>([]);
  const [live, setLive] = createSignal(options.live ?? true);
  const listeners = new Set<(decision: Decision) => void>();

  streamQueries((decision) => {
    for (const listener of listeners) {
      listener(decision);
    }
    if (live()) {
      setEntries((prev) => [entryFrom(decision), ...prev].slice(0, cap));
    }
  });

  return {
    entries,
    live,
    setLive,
    load: async (filter) => {
      const page = await listQueries({ ...filter, limit: filter.limit ?? 2000 });
      setEntries(page.queries.map(normalize));
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

// The stream stamps time as RFC 3339 and the log endpoint as unix millis, so
// both boundaries normalize to ISO strings here.
function normalize(entry: QueryEntry): QueryEntry {
  return { ...entry, time: toIso(entry.time) };
}

function entryFrom(decision: Decision): QueryEntry {
  return {
    time: toIso(decision.time),
    client: decision.address,
    name: decision.name,
    type: decision.type,
    verdict: decision.action,
    rule: decision.rule?.id,
  };
}

function toIso(time: string | number): string {
  if (typeof time === "number") {
    return new Date(time).toISOString();
  }
  const parsed = Date.parse(time);
  return Number.isNaN(parsed) ? time : new Date(parsed).toISOString();
}
