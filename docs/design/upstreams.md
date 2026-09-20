# Upstream management

This records the shape of `internal/upstream` and what it deliberately does
not do. It is the design record for #21's first landing: several resolvers,
DoT and DoH on top of plain UDP, health scoring, and failover; and for #77's:
stored upstreams, per-domain and per-client routing, and the surfaces they
show on.

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

## The cache keys on the question, and on the route

One shared cache sits over the pool, keyed on name, class, and type — and on
the route once routing exists. A query a route sends to a private resolver is
a different question from the same name sent to a public one, so the key
gains the route and every entry remembers the route it was fetched with,
prefetch included. A setup without routes keys everything on the empty route
and behaves exactly as before. Resolvers that disagree about a name still
share one answer whenever no route separates them; keying every entry by
upstream would multiply the table to defend against a split this product
only expresses through explicit routes, and routes already carry the key.

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

## The store owns the resolvers

Upstream rows live in the `upstreams` table: a name, the canonical URL, an
enabled flag, and a backup flag. The `--upstream` flag seeds an empty table
only, whatever the boot, so a database upgraded from a flag-only release gets
its resolvers once and the store owns them from then on. The API serves CRUD
on `/api/v1/upstreams`, and every write reloads the same way a rewrite write
does, so an edit reaches a running server without a restart.

The runtime publishes a fresh pool on every reload through `upstream.Switch`,
a stable handle the cache wires to once at boot. A reload that cannot build a
pool leaves the previous one serving; a store with no enabled upstream fails
the same way an empty flag list always did, and the API refuses the write
that would cause it.

## Routing

A route row sends matching queries to one named upstream: an optional client
(the identity key), an optional domain (the name itself or anything under
it), and the upstream's name. The runtime builds the router into the same
snapshot as the rule set and the identity table, so one atomic load decides
filter, identity, and route together and a query cannot be routed by an old
table while filtered by a new one. The route name rides the filter's verdict
to the handler, through the cache, and onto the pool.

A routed query resolves through that one peer only. It never fails over to
another resolver, because the point of sending a name to a private resolver
is that a public one would answer it wrongly, and a leaked split-horizon
answer poisons the shared cache. An operator who wants a routed name to have
spare capacity lists that upstream several times under different names.

Match order, first match wins: a client-scoped route outranks a
domain-scoped one, a deeper domain outranks a shallower one, and the row id
breaks the rest. An empty domain matches every name; an empty client matches
every client. The API refuses a route naming an unknown or disabled
upstream, an unknown client, or a domain no query can carry, and refuses
deleting an upstream or client a route still references: a route that
vanishes or dangles under the operator is a silent policy change.

Backup upstreams never enter the candidate order while a regular peer is
eligible, so a backup is probed only when everything ahead of it is down.

## Divergences from AdGuard Home, cited

- Per-client upstreams in AdGuard Home replace a client's whole resolver
  list. Aegis routes name one upstream per rule and stack by specificity,
  which is the dnsmasq `server=/domain/ip` shape AdGuard lacks. An operator
  who wants AdGuard's per-client list adds one client-scoped route per
  resolver.
- AdGuard Home has no per-domain routing. The superset is deliberate.
- Backup upstreams generalize AdGuard's global fallback list into a per-row
  flag; a fallback list is the degenerate case where every row is backup.
- Per-category routing waits until Aegis has a category concept to route on
  (blocked services, threat intel). Routing half of one now would invent a
  domain-set type the rest of the product does not share.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| pool ranked by health | ordered failover, EWMA latency | chosen |
| race every resolver | first answer wins | rejected, multiplies upstream load on every query and hides which resolver served |
| fail over on SERVFAIL too | retry next resolver on any non-answer | rejected, an upstream that answered is healthy and broken names would hit every peer |
| background health prober | periodic cheap queries | rejected, passive recovery probes with real traffic and costs nothing when idle |
| cache keyed by upstream | one table per resolver | rejected; the route joins the key when routing exists, which it now does |
| routed queries fail over to the pool | next resolver after the routed one errors | rejected, a public resolver answers a split-horizon name wrongly and the cache keeps the poison |
| route targets a resolver group | a route names an ordered list | rejected until asked; one named upstream per route keeps the table and the UI honest, and listing a resolver twice under two names is the escape hatch |
| persistent connections | cached `*dns.Conn` per peer | deferred, the forwarder never had them and the cache absorbs repeats |

## Deferred

- **Health on a surface.** Latency averages and failure counts live on the
  pool; #56's metrics and the graph's multi-upstream stars are where they
  show.
- **DoQ.** Needs `quic-go`; the issue says it waits for a reason.
