# Response cache

This records the shape of `internal/cache` and what it deliberately does not do.

## The decision

The cache is a decorator over the upstream resolver, the same `Resolve(ctx,
req)` shape the DNS handler already consumes. It sits between the handler and
the forwarder, so it only ever sees queries the filter allowed. Per-client
policy stays ahead of the cache and never goes stale inside it. It holds an
LRU table keyed on lowercased name and record type, bounded by entry count
(default 4096), with one mutex over the table and the order.

An entry stores a private copy of the upstream answer with the extra section
dropped. Extra carries the upstream's EDNS state, which has no meaning on a
replay to another client. A hit rewrites the ID and the question from the
incoming request and decays every TTL by the entry's age, so a client never
sees the full TTL twice and the answer is not distinguishable from a forward
on the wire.

## TTL policy

A positive answer lives for the shortest answer TTL. A negative answer (NXDOMAIN
or an empty success) lives for the SOA negative TTL of RFC 2308, the shorter of
the header TTL and the SOA minimum. An answer with no TTL guidance, and every
rcode that is neither success nor name error, is not cached, so a SERVFAIL is
retried on the next query.

Both bounds clamp before storage: the floor (default 5s) keeps a zero-TTL name
from reaching the upstream on every query, the ceiling (default 1h) bounds how
long an answer can outlive an upstream change. The stored record headers carry
the clamped value, not the raw one, and the SOA minimum moves with it, so a
stub caching an answer for itself stays in step with what we served.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| decorator over Resolver | LRU table, RFC 2308 TTL policy | chosen |
| cache inside the handler | check after the filter, before forward | rejected, the handler's value is that it holds no mutable state |
| dns.Msg cache keyed on the wire bytes | key on the raw question string | rejected, it would serve a different qtype or class from the wrong slot when a client varies the bits the key ignored |

The handler alternative would also have given the cache the power to answer a
blocked name from an old entry, since the verdict would arrive after the
lookup. Keeping the filter strictly ahead removes that class of bug by
construction.

## Deferred

- **Prefetch of popular names.** No popularity signal is acted on yet. The
  query log holds the counts, but reading it from the cache couples two
  packages around data neither owns. Land it when a refresh story exists.
- **Hit-rate counters.** Metrics do not exist (#56). Exposing the rate is a
  one-liner once there is a surface to expose it on.
- **Singleflight for concurrent misses.** Two clients asking for the same cold
  name both go upstream today. The herd is small at sinkhole scale; add it
  when a trace shows it matters.
