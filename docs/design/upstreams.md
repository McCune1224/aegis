# Upstream management

This records the shape of `internal/upstream` and what it deliberately does
not do. It is the design record for #21's first landing: several resolvers,
DoT and DoH on top of plain UDP, health scoring, and failover.

## The decision

The pool owns every configured resolver and implements the same `Resolve`
signature the single `Forwarder` had, so the cache decorates the pool the way
it decorated the forwarder and the handler does not change. Per peer the pool
keeps three fields: a moving average of successful exchange latency, a count
of consecutive failures, and a `downUntil` timestamp.

One exchange is one attempt with its own timeout (default 2s) inside the
caller's context, so one dead resolver costs one budget, not the client's
whole patience. A query walks candidates in health order and returns the
first answer. Two consecutive failures mark a peer down with an exponential
backoff (10s base, doubling, 10 minute cap). Recovery is passive: when the
backoff elapses the peer is eligible again and real queries probe it, so
there is no background prober to feed and no probe traffic to an upstream the
operator replaced. When every peer is down, the pool still tries the
best-ranked one, so a pool of dead resolvers degrades to today's behaviour
instead of inventing a new hard-failure mode.

Candidates are ranked by the latency average, unmeasured peers first so a new
resolver gets probed once, ties broken by configured order. Failure counts
shape eligibility, not rank.

A SERVFAIL answer passes through untouched. It came from a resolver that is
up, and failing over on it would multiply load for every permanently broken
name on the internet. Failover answers transport errors only.

## The cache keys on the question

One shared cache sits over the pool, keyed on name, class, and type as
before. Resolvers that disagree about a name therefore share one answer, and
whichever resolver served it wins. For a sinkhole pointed at public
resolvers this is the right trade: keying by upstream multiplies the table
and doubles misses to defend against a split-horizon setup this product does
not yet express. If routing below introduces per-query upstream choice, the
cache key gains the route at the same time.

## Transports and URLs

An upstream is a URL, parsed in this package and trusted after it.

| Form | Meaning | Default port |
| --- | --- | --- |
| `host:port` or `host` | plain UDP, the historical flag value | 53 |
| `udp://host[:port]` | plain UDP | 53 |
| `tcp://host[:port]` | plain TCP | 53 |
| `tls://host[:port]` | DNS over TLS (`miekg/dns` `tcp-tls`) | 853 |
| `https://host[:port]/path` | DNS over HTTPS, RFC 8484 POST | 443 |

A truncated UDP answer is retried over TCP to the same upstream, which the
forwarder always did and the UDP transport keeps. DoT and DoH run over
stream transports, so they never truncate. `http://` is rejected outright:
plaintext HTTP DNS is a footgun, and plain DNS already covers the insecure
case. DoQ waits on `quic-go`, the heavy dependency the issue deferred.

## Configuration

`--upstream` is repeatable and every value is a URL. The default stays one
plain UDP resolver, so upgrading changes no behaviour, and `AEGIS_UPSTREAM`
keeps working as the single-value environment form. The store and API do not
hold upstreams yet, so a change still needs a restart; #21's remaining slice
(stored upstreams, per-domain and per-client routing, health on the API and
graph) builds on this pool and tracks in its own issue.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| pool ranked by health | ordered failover, EWMA latency | chosen |
| race every resolver | first answer wins | rejected, multiplies upstream load on every query and hides which resolver served |
| fail over on SERVFAIL too | retry next resolver on any non-answer | rejected, an upstream that answered is healthy and broken names would hit every peer |
| background health prober | periodic cheap queries | rejected, passive recovery probes with real traffic and costs nothing when idle |
| cache keyed by upstream | one table per resolver | rejected until routing exists, see above |
| persistent connections | cached `*dns.Conn` per peer | deferred, the forwarder never had them and the cache absorbs repeats |

## Deferred

- **Stored upstreams and routing.** Per-domain, per-client, per-category
  routing needs the store schema and the identity table in on the decision,
  so it lands on top of this pool rather than inside it.
- **Health on a surface.** Latency averages and failure counts live on the
  pool; #56's metrics and the graph's multi-upstream stars are where they
  show.
- **DoQ.** Needs `quic-go`; the issue says it waits for a reason.
