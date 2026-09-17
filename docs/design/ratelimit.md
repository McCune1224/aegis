# Per-client rate limits

This records the shape of `internal/ratelimit` and what it deliberately does
not do.

## The decision

The limiter holds one `x/time/rate` token bucket per client identity. The
handler asks it on the allowed path only, after the filter verdict and before
the upstream resolver, so blocked queries never touch it and the cache sits
behind it: a flood of names the cache could answer is still a flood of
queries. The check is one map lookup and one atomic spend.

The bucket is keyed on the resolved identity, not the source address. That
keeps the bucket table bounded by the client table plus one default bucket,
with no cleanup story, and it groups devices the way policy already groups
them. The cost is that every unclaimed address on a network shares one
default bucket, so one flooding device behind an unclaimed NAT edge can
starve its neighbours. The documented answer is to claim the edge as a client,
which gives it a bucket of its own.

An over-limit query is refused with REFUSED and counted. It is not written to
the query log: the point of the limit is that a flood stops costing resources,
and a log row per refused query hands the flood a second target. The count
lives on the limiter and surfaces when #56 lands, the same way the cache's
hit rate waits on it.

## Configuration

`--rate-limit` is queries per second per identity, `--rate-burst` is how many
one identity may spend in an instant. Both default to zero, which disables
rate limiting, so upgrading changes no behaviour. A burst without a rate is a
configuration error, not an ignored flag. Per-profile limits are deferred
until the profile surface grows a place to put them.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| bucket per resolved identity | one table, bounded by the client table | chosen |
| bucket per source address | LRU of buckets with expiry | rejected, unbounded under address rotation and one lock per cleanup |
| limiter inside the cache or forwarder | gate at the resolver | rejected, neither holds the client address |
| limiter inside the Decider | a refused verdict | rejected, it would rate-limit the block path and mix rate policy into rule verdicts |
| drop over-limit queries | no reply | rejected, REFUSED tells the client to back off instead of waiting out a timeout |

## Deferred

- **Per-profile rates.** One global rate today; a profile column comes when
  the profile CRUD grows a field for it.
- **Refusal counters on a surface.** `Limiter.Refused` exists; #56 names where
  it is exposed.
