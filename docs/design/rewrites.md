# Rewrites and local records

This records the shape of `internal/rewrite` and what it deliberately does
not do. It is the design record for #24.

## The decision

A rewrite maps a name to an address or to another name. The table answers
one question before the filter sees the query, which is what makes a
homelab name resolve no matter what the blocklists say about it.

Exact and wildcard patterns live in one table. A wildcard is a leading
`*.`, and it matches any subdomain at any depth, the way AdGuard Home
behaves: `*.example.com` matches `a.example.com` and `a.b.example.com`, and
not the apex `example.com` itself, which needs its own exact rewrite. When
two patterns match, the longer suffix wins, so `sub.example.com` can pin
one host out of a wildcard.

A PTR answer is generated from exact address rewrites, in the direction the
operator wrote them: `home.local -> 192.168.1.50` also answers
`50.1.168.192.in-addr.arpa` with `home.local`. Wildcards are left out;
an address matched by a pattern is not one host, so a reverse answer for
it would be a guess. IPv6 reverse names in `ip6.arpa` nibble form work the
same way.

## Address and name targets behave differently on purpose

An address rewrite answers locally. The query never reaches the filter or
the upstream, because the operator pinned the name to an address and a
block rule against that name is a configuration fight the operator already
lost on purpose. The answer carries a fixed 60 second TTL, short enough
that removing the rewrite takes effect without waiting out a cache.

A name rewrite returns a CNAME and then resolves the target through the
whole pipeline again: rewrites first, so chains work, then the filter, then
the upstream. This is the rule the done-when asks for: the rewritten name
is still matched against the rules. There is no flag to skip the filter on
a name rewrite; if a target must resolve regardless, rewrite the target to
an address too.

A query whose type does not match the address family gets an empty success,
the same NODATA rule the block answers use: A of an IPv4 target answers,
AAAA of an IPv4 target answers empty.

## The query log

A rewritten query logs with verdict `rewrite` and the rule column carries
the target. The `queries` table needs no migration; `verdict` is already
text. Address rewrites are answers, not upstream work, so the rate limiter
does not see them, and neither does the cache.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| rewrites inside the filter engine | a verdict kind | rejected, the filter decides allow and block and a local answer is neither |
| wildcard matches one label only | the blocklist matcher's rule | rejected, AdGuard parity means any depth and operators will import their old patterns |
| PTR from wildcards too | best-effort reverse | rejected, a wildcard address is not one host and a wrong PTR is worse than none |
| filter flag per rewrite to skip blocking | one more column | rejected until asked, address rewrites already bypass the filter and name rewrites should not |

## Deferred

- **Per-rewrite upstream.** AdGuard can send a rewritten query to a chosen
  resolver. That is a routing decision and waits on #77, where upstream
  choice per query is designed once.
- **Web panel and API CRUD.** The engine, store, and flag seed land first;
  the surface follows the same week, before this leaves flag-only
  configuration.
