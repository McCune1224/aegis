# Blocked services

AdGuard Home ships curated domain sets per online service and blocks one with a
click. This records how Aegis gets the same sets, how an operator enables one,
and where the two products differ.

## The decision

A service is a catalog entry with an id, a name, a group, an inline icon, and
the rules that carry it. Aegis fetches the catalog AdGuard publishes for its own
build step:

    https://adguardteam.github.io/HostlistsRegistry/assets/services.json

and stores it in `services`. Two tables hold the enablements:
`profile_services` (which profile blocks which service) and `client_services`
(which client blocks which service for itself). A reload turns every enablement
into ordinary `filter.RuleSpec`s with `Action: Block` and a `Source` naming the
service. A profile enablement carries `Profile`, a client enablement carries
`Client`, so verdicts, provenance, and the query log keep one code path with
every other rule.

The alternative was a generated Go file per release, which is what AdGuard Home
does. It was rejected because the sets change between releases: an operator
should not wait for Aegis to tag a version to block a service that appeared last
week. The cost is that an address with no network on first boot has an empty
catalog until the fetch succeeds, so the stored copy keeps serving and a failed
refresh is a warning rather than a failed start.

## How the catalog dialect is read

The catalog uses the AdGuard rule dialect. `internal/services` parses the
subset that has a DNS meaning, because the general-dialect parser in
`internal/blocklist` also accepts allow rules and lists that this document never
carries:

| Rule | Meaning here |
| --- | --- |
| `\|\|host^` | the host and every name under it |
| `\|host^` | exactly that host, which is how the catalog names a CDN edge |
| `/pattern/` | a regular expression, taken as written |
| a rule with a `*` label | a wildcard, when every `*` is a whole label |

A rule the dialect cannot express is counted as skipped rather than failing the
service, the way a list import counts a line. `*` also appears mid-label in the
published catalog, as in `ebay-*.s3-us-west-1.amazonaws.com`; Aegis wildcards
stand for whole labels, so those rules carry no meaning here and are among the
skipped. Of 2395 rules in the catalog as published, 2379 become rules and 16 are
skipped.

## Where Aegis diverges from AdGuard Home

**The layers add instead of override.** AdGuard Home keys blocked services on
the client and keeps one global set beside it; a client with its own list uses
that list instead of the global one. Aegis has no global set. A client's
profile is its base layer, and the client's own enablements add to it, the same
rule the time windows follow (see docs/design/windows.md). Turning a service off for one device against a
profile that blocks it needs an allow rule, which the filter engine already
expresses; the services page does not hide that from the operator, the query
log names the service through the rule's `Source` either way.

**The catalog is fetched, not generated.** AdGuard Home builds its sets into
the binary at release time. Aegis stores the same public JSON and refreshes it
on a background interval, so a set an operator enables is current between
releases. The trade is a first-boot fetch and an offline first boot that has no
services until the catalog arrives.

**The dialect is a declared subset.** AdGuard Home matches the catalog rules
against every query with its general rule engine. Aegis converts the subset with
a DNS meaning into indexed name rules and skips the rest, so the per-query cost
stays the same as any other rule and the skipped count is a number an operator
can see rather than a rule that quietly never fires.

## How a refresh reaches the running server

`POST /api/v1/services/refresh` fetches the catalog, writes it over the stored
copy in one transaction, and republishes the rule set. A background loop calls
the same function once at boot and once a day. A fetch or parse failure leaves
the stored catalog serving, so an unreachable catalog cannot empty what an
operator already enabled. A service the catalog no longer carries keeps its
last rules and its enablements, so a catalog that drops a service does not
silently unblock it.

## Deferred, with the reason

Pruning the catalog when the upstream document drops a service. Keeping the row
costs a handful of stale rules and no correctness; pruning would either cascade
the enablements away, silently unblocking a service an operator chose, or fail
the whole refresh. Revisit if the stored catalog ever grows enough to matter.
