# How to test aegis

A change needs evidence from the surface it touches. "It compiles" is not
evidence. This document records the standard every test in this repo must meet,
the audit that enforces it, and the evidence each kind of change needs.

## The standard

1. **Tests drive public surfaces.** A test calls the package the way `main`
   calls it, or the server the way a client does: real HTTP on an ephemeral
   port, real SQLite in a temp file, real DNS over UDP and TCP. A test that
   asserts which internal function ran, or restates a constant the code
   already holds, is deleted on sight: it still passes when everything it
   imports returns nil.
2. **No mocks of code we own.** The seams between our packages run for real in
   tests. Doubles are only for what we do not own and cannot run in CI: the
   internet, the clock, hardware.
3. **Assertions carry exact values.** A hand-computed expected value, not
   "not nil", "not empty", or "greater than zero". A weak assertion is the
   test equivalent of no test: it survives any bug that is not a crash.
4. **Pure tests only for pure logic.** Parsers, matchers, health math,
   aggregation, and geometry get fast in-memory tests because that is their
   whole surface. Anything with I/O, a cache, a schedule, or a socket is
   tested through the layer where that I/O exists.
5. **A deleted test is a victory.** Every test earns its runtime and its
   maintenance. When a test breaks on a refactor that preserved behaviour, it
   was mirroring the implementation. Delete it, or replace it with one that
   drives a public surface.

## The audit

```
make mutants            # whole tree
go tool gremlins ...    # not this; use the tool from make tools
~/go/bin/gremlins unleash ./internal/filter
```

Mutation testing flips operators and conditions in the source and reports
which mutants the tests kill. It is the objective answer to "are these tests
doing anything":

- **Lived** mutants are the conviction: the suite ran and did not notice a
  behaviour change. Fix the weak assertions, or delete the test they live in.
- **Not covered** mutants name behaviour no test reaches. Either the behaviour
  is unreachable trivia and the code should go, or it needs a test at the
  lowest layer where it can be observed.
- **Timeouts** mean the mutant hung the suite, which counts as detected.

Two caveats the first audit taught:

- Socket-tier packages (real stubs, real SQLite) time out mutants
  non-deterministically, so their per-run score wobbles. The pure tier is
  where the score is exact; a pure package must hold zero lived mutants.
- Some survivors are accepted classes and are not worth chasing: log-only
  branches, `ctx.Err() == nil` guards, nil-guards for optional wiring, mutant
  arithmetic on constant declarations, and equivalent mutants such as flipping
  the sign inside a square. Everything else with a name in the output gets
  fixed or the test gets deleted.

## The tiers

Five tiers. A change needs evidence from the tier its behaviour lives in, and
most work stops at the second one. Only the last tier needs different hardware.

### Tier 1. Pure tests

No sockets, no files. Runs in milliseconds. Reserved for packages whose entire
surface is pure: `internal/filter` matching, `internal/blocklist` parsing,
`internal/services` catalog dialect, `internal/threat` scoring math, the web
graph's geometry and statistics.

```
go test ./internal/filter ./internal/blocklist
```

### Tier 2. Local sockets

Real UDP and TCP on `127.0.0.1` with port `0`, so the kernel picks the port and
tests never collide. The API harness in `internal/api` starts the real server
against a real SQLite file and asks questions over real DNS; a stub upstream
answers what a block lets through.

```
go test ./internal/api ./internal/dns
```

This tier is where per-client policy, services, rewrites, and the query path
get checked. Prefer it over any mock: the wire bytes are what a real client
sees.

### Tier 3. A virtual network with several clients

Per-client policy needs distinct source addresses. A rootless network namespace
gives you a throwaway one, with no sudo:

```
unshare -rn sh -c '
  ip link set lo up
  ip addr add 10.9.9.1/24 dev lo
  ip addr add 10.9.9.2/24 dev lo
  ip addr add 10.9.9.3/24 dev lo
  exec "$SHELL"
'
```

Inside that namespace, `10.9.9.1` is the server and `10.9.9.2` through
`10.9.9.4` stand in for separate devices. `dig -b 10.9.9.2 @10.9.9.1 -p 15353`
selects which one asks. The namespace disappears when the shell exits.

This is the check that proves per-client policy end to end against the real
binary. Run aegis with two profiles and ask the same blocked name from two
addresses:

```
./bin/aegis serve \
  --dns-address 10.9.9.1:15353 \
  --upstream 127.0.0.1:1 \
  --db /tmp/aegis.db \
  --blocklist ./list.txt \
  --profile kids=refused \
  --client 10.9.9.2=kids \
  --log-level error &

dig -b 10.9.9.2 @10.9.9.1 -p 15353 ads.example.com   # REFUSED, the kids profile
dig -b 10.9.9.3 @10.9.9.1 -p 15353 ads.example.com   # NXDOMAIN, the default
```

The upstream is unreachable on purpose. A blocked name never reaches it, so the
check needs no working resolver and no internet access.

### Proving the database is the source of truth

The first boot seeds the database from the flags. Every boot after that ignores
them, which is what lets the web app take over without a flag overwriting it.
Run the server twice against one database, the second time with no profile or
client flags at all, and confirm the answers do not change.

### Tier 4. Cross-architecture build

```
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o /tmp/aegis-armv7 ./cmd/aegis
CGO_ENABLED=0 GOOS=linux GOARCH=arm64  go build -o /tmp/aegis-arm64 ./cmd/aegis
file /tmp/aegis-armv7 /tmp/aegis-arm64
```

GNU `file` reports `ELF 32-bit LSB executable, ARM, EABI5` and `ELF 64-bit LSB
executable, ARM aarch64`, both statically linked. Confirm that output rather
than trusting a green build: a CGO dependency would silently produce a dynamic
binary that fails on a Pi with a different libc.

### Tier 5. Real ARM hardware

The Pi answers two questions the other tiers cannot: whether the binary runs on
32-bit ARM at all, and what the filter costs on that CPU. AdGuard Home keeps
port 53 there permanently; aegis runs beside it on a high port and never cuts
over.

```
scp /tmp/aegis-armv7 pi:~/aegis
ssh pi '~/aegis serve --dns-address 0.0.0.0:15353 --upstream 9.9.9.9:53'
dig -p 15353 @<pi-address> ads.example.com
```

## The web UI

The node graph renders to a WebGL canvas with no DOM to query, so its
geometry, layout, and statistics live in pure modules (`stats`, `label`,
`topology`, `flow`) with exact-value tests, and the rendered result is judged
by screenshot. Page interactions are driven over CDP against `make dev` or the
built binary.
