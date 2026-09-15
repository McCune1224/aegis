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
4. **DNS handler.** Turn a verdict into a wire response and apply the four
   blocking modes, with the upstream behind a `Resolver` seam.
5. **DNS server and forwarder.** Bind UDP and TCP, and forward allowed queries to
   a configured upstream.
6. **More matchers.** Wildcard, regular expression, CIDR, and client. Each is a
   new match kind rather than a new branch in `Decide`.
7. **Profiles.** Inheritance resolved at compile time, with cycles rejected. The
   check is that a child setting wins and that a cycle fails to compile.
8. **Schedules.** Compile windows into a minute-of-week table.
9. **Store.** SQLite through sqlc and goose, holding sources, rules, profiles,
   clients, and the query log.
10. **HTTP API.** chi routes plus SSE for the live query stream.
11. **Web UI.** The Solid 2 shell first, then the node graph.

The matcher work moved from fourth to sixth and the DNS work moved up. The
strongest check available for everything built so far is a real DNS query, and
more matchers add breadth to a system nobody can run yet. The handler comes
first because it is the part that is pure and cheap to test on its own.

## Deferred, with the reason

Hand-written DFAs for wildcard and regular expression rules. Go's `regexp` is
fast enough for the small number of regex rules a home network carries. Revisit
only when a measurement asks for it.

`quic-go` and DNS-over-QUIC. A large dependency for one transport, and not on the
path to replacing AdGuard Home for a first user.

Blocklist diffing and source health monitoring. Both need the store first.
