# Response cache

This records the shape of `internal/cache` and what it deliberately does not do.

## The decision

The cache is a decorator over the upstream resolver, the same `Resolve(ctx,
req)` shape the DNS handler already consumes. It sits between the handler and
the forwarder, so it only ever sees queries the filter allowed. Per-client
policy stays ahead of the cache and never goes stale inside it. It holds an
LRU table keyed on lowercased name, class, and record type, bounded by entry
count (default 4096), with one mutex over the table and the order.

An entry stores a private copy of the upstream answer with the extra section
dropped. Extra carries the upstream's EDNS state, which has no meaning on a
replay to another client. A hit rewrites the ID and the question from the
incoming request and decays every TTL by the entry's age, so a client never
sees the full TTL twice and the answer is not distinguishable from a forward
on the wire.

Hits, misses, bound evictions, and started prefetches are counted under the
same mutex as the table and read through `Stats()`. The counters land in this
package because that is where the LRU already runs; a metrics endpoint (#56)
or an API field reads the snapshot, it does not count.

## Prefetch

A hit inside the last fifth of an entry's stored life arms one background
refresh for that slot: the client is answered from the stored entry and the
upstream is re-asked behind it, so a popular name never costs a client the
cold round trip. Three properties are deliberate:

- **Demand-driven.** Nothing is refreshed unless a client asked. A quiet
  network spends no upstream traffic; the query rate is the refresh budget.
- **One in flight per slot.** A `refreshing` flag deduplicates the window, so
  a burst of asks cannot stampede the upstream. A failed refresh frees the
  flag, and the next ask retries it — one try per ask.
- **No stale answers.** The refresh runs on its own context (10s timeout),
  never the asking client's, and swaps the stored entry only on a cacheable
  answer. A failure leaves the stored entry to live out its own TTL.

The window is a fraction of the stored TTL, not a fixed second count, so it
scales with how long the answer is actually good for: an hour-clamped entry
refreshes in its last twelve minutes, a floor-clamped one in its last second.

This differs from AdGuard Home's optimistic cache, which answers from an
entry even after its TTL has run out and repairs it in the background. Aegis
refreshes before expiry instead, so no answer is ever replayed past the TTL
its headers promised and the TTL-decay replay guarantee stays intact. The
cost is one background exchange per popular name per TTL instead of a longer
stale window — accepted, because the sinkhole's answers are only as credible
as their TTLs.

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
| background sweeper over the query log | refresh popular names unconditionally | rejected, it couples the cache to the query log around data neither owns and spends upstream traffic on a quiet network; demand-driven arming gets the popularity signal for free |

The handler alternative would also have given the cache the power to answer a
blocked name from an old entry, since the verdict would arrive after the
lookup. Keeping the filter strictly ahead removes that class of bug by
construction.

## Deferred

- **Singleflight for concurrent misses.** Two clients asking for the same cold
  name both go upstream today. The herd is small at sinkhole scale; add it
  when a trace shows it matters. (The prefetch refresh already deduplicates
  per slot; this is about the cold-miss path.)
