# Filter engine

This records the shape of `internal/filter` and the order the rest of the phase
one work lands in.

## The decision

`internal/filter` compiles rules once into immutable tables and publishes a new
set by swapping one pointer. A query reads the current set and never takes a
lock.

The alternative was a middleware chain over a mutable per-query context. That
loses because resolution order becomes a runtime property, so it can only be
tested by running queries, and because any stage on the chain can quietly change
the answer.

## How the decision was reached

Three designs ran in parallel against one brief, each on a different model
family.

| Candidate | Shape | Outcome |
| --- | --- | --- |
| chain | middleware over a mutable context | failed, the provider rejected the request |
| table | immutable compiled table, atomic publish | base |
| graph | policy graph compiled to materialized tables | ran, but the workflow timed out before reporting |

The base is the table candidate. Two ideas crossed over from the graph
candidate.

**Tier ordering.** Precedence is a table rather than a branch, so an allow rule
wins over a block rule by comparison. A later tier for rewrites or client
overrides is one more row.

**Compile the schedule, do not evaluate it.** The graph candidate folds
recurrence and overlap into a total function from local minute-of-week to a
policy. Per query that is one array index. This lands with the schedule unit,
and keying on local time is what makes "no games after 21:00" mean what the
operator meant.

The graph candidate also made a product point worth keeping. The compiled spec
is the same structure the node graph dashboard renders, so the operator edits
the graph the compiler consumes and there is no second representation.

## What phase one ships

Each unit ends in a check and lands as its own commit.

1. **Rule matching.** Exact and subdomain rules, allow beats block, provenance on
   the verdict. Done. 30 ns per decision against 10 rules and 34 ns against
   100000, both at zero allocations.
2. **Rule set swap.** An `Engine` holding one atomic pointer with `Publish`. Done.
   The check is a test that a query in flight sees one consistent set.
3. **Blocklist parsing.** Parse hosts files, AdBlock syntax, and domains-only
   lists into `RuleSpec`. Done. The check is one fixture per format with a
   literal expected `[]RuleSpec`.
4. **DNS handler.** Turn a verdict into a wire response and apply the blocking
   modes, with the upstream behind a `Resolver` seam. Done.
5. **DNS server and forwarder.** Bind UDP and TCP, and forward allowed queries to
   a configured upstream. Done.
6. **Profiles and client identity.** Inheritance resolved at compile time, with
   cycles rejected, and an address resolved to an identity outside the filter
   package. Done. The check is that two addresses asking the same blocked name
   get two different answers, proven in a rootless namespace per
   `docs/testing.md` tier 3.
7. **Store.** SQLite through sqlc and goose, holding profiles, clients, and the
   selectors that identify them. Done. The check is a round trip through a real
   file that ends in a verdict and a resolved identity.
8. **Runtime.** One owner that rebuilds the engine from the store and publishes,
   so a stored change reaches a running server without a restart. Done. The rule
   set and the identity table live in one snapshot, because holding them in two
   would let a query pair the new rules with the old selectors during a reload.
9. **HTTP API.** chi routes over the store, plus SSE for the live query stream.
10. **Web app.** The Solid 2 shell, then the configuration screens, then the
    node graph.
11. **Query log.** Persist each decision and serve it to the dashboard.
12. **More matchers.** Wildcard, regular expression, and CIDR rules.
13. **Schedules.** Compile windows into a minute-of-week table.

## Why the order changed twice

The DNS work moved ahead of the matchers, because a real DNS query is the
strongest check available for everything built before it, and more matchers add
breadth to a system nobody can run yet.

The store, the API, and the web app then moved ahead of the matchers and the
schedules, because the web app is the configuration surface the product is built
around. A flag per setting does not scale to per-client profiles and per-device
selectors, and the command line is now a bootstrap path rather than the way an
operator configures Aegis. The store is the source of truth, and the YAML file
this project originally planned is now only a way to seed a first boot.

## How a client is identified

A client is a name plus the selectors that carry it, because one device answers
on more than one address and a whole network can share a policy. Selectors
resolve with an explicit precedence, and the first one that matches wins.

1. An exact address.
2. The longest matching prefix.

An exact address beats a prefix, because pinning one address is deliberate. The
longest prefix wins, because it is the most specific network. Two identities
claiming the same selector is a load error rather than a silent last-one-wins,
because two policies fighting over one device is the thing an operator cannot
debug from the outside.

An address nothing claims gives the empty key, which takes the default profile.

## Deferred, with the reason

Hand-written DFAs for wildcard and regular expression rules. Go's `regexp` is
fast enough for the small number of regex rules a home network carries. Revisit
only when a measurement asks for it.

`quic-go` and DNS-over-QUIC. A large dependency for one transport, and not on the
path to replacing AdGuard Home for a first user.

Blocklist diffing and source health monitoring. Both need the store first.
