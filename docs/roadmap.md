# Roadmap

Where this is, what is next, and what is left. Every unit below is a GitHub issue
with its own acceptance check.

## Where it stands

The backend resolves DNS and blocks by rule, with per-client policy, from a
SQLite database that is the source of truth. Ten pull requests are merged, there
are 87 tests across eight packages, and `make test`, `make lint`, and
`make crossbuild` are green.

What works today:

```
aegis serve --dns-address 0.0.0.0:53 --upstream 9.9.9.9:53 \
  --db aegis.db --blocklist ~/list.txt \
  --profile kids=refused --client 10.0.0.5=kids
```

The flags seed an empty database on first boot. After that the database is the
source of truth, and the flags are ignored, which is what lets the web app take
over without a flag overwriting it on restart.

What is missing is the reason anyone would want to: the API and the web app. The
backend is one unit away from serving a configuration screen.

## Next

In order, because each needs the one before it.

| Issue | What | Why now |
| --- | --- | --- |
| [#11](https://github.com/McCune1224/aegis/issues/11) | HTTP API for profiles and clients | `runtime.Reload` exists and nothing calls it |
| [#12](https://github.com/McCune1224/aegis/issues/12) | Live query stream over SSE | the dashboard needs it, and the choice is already made |
| [#13](https://github.com/McCune1224/aegis/issues/13) | Web app shell, built and embedded | the bundle has to load before anything can be built on it |
| [#14](https://github.com/McCune1224/aegis/issues/14) | Profile and client screens | the first thing a person can actually use |
| [#15](https://github.com/McCune1224/aegis/issues/15) | Admin access control and HTTPS | the gate before this touches a real network |

Stop after #15 and the product is usable. Everything past it is making it good.

## The full map

### Backend

- [#16](https://github.com/McCune1224/aegis/issues/16) Blocklist sources: fetch a list from a URL
- [#17](https://github.com/McCune1224/aegis/issues/17) Source refresh, health, and a diff preview
- [#18](https://github.com/McCune1224/aegis/issues/18) Custom allow and block rules in the store
- [#19](https://github.com/McCune1224/aegis/issues/19) Query log: persist and serve
- [#20](https://github.com/McCune1224/aegis/issues/20) Dashboard: analytics and the live log
- [#21](https://github.com/McCune1224/aegis/issues/21) Upstream management: several resolvers, DoH, DoT, health, failover
- [#28](https://github.com/McCune1224/aegis/issues/28) CLI: import and export

### The differentiators

The original brief was AdGuard Home with more, and more personal. These are the
parts that are more.

- [#25](https://github.com/McCune1224/aegis/issues/25) The node graph
- [#26](https://github.com/McCune1224/aegis/issues/26) Threat intelligence and query pattern analysis
- [#22](https://github.com/McCune1224/aegis/issues/22) More matchers: wildcard, regular expression, CIDR
- [#23](https://github.com/McCune1224/aegis/issues/23) Schedules: policy by time of day
- [#24](https://github.com/McCune1224/aegis/issues/24) DNS rewrites and local records
- [#27](https://github.com/McCune1224/aegis/issues/27) DHCP server, MAC-based identity, and client discovery

### Build, release, deployment

- [#29](https://github.com/McCune1224/aegis/issues/29) Test the release matrix for real
- [#30](https://github.com/McCune1224/aegis/issues/30) Docker images
- [#31](https://github.com/McCune1224/aegis/issues/31) Validate on real ARM hardware

### Tracked debt

These are gaps found while building, not features. Each names what it would take
to close.

- [#32](https://github.com/McCune1224/aegis/issues/32) No independent review on any merged PR
- [#33](https://github.com/McCune1224/aegis/issues/33) The benchmark regression is not fully explained
- [#34](https://github.com/McCune1224/aegis/issues/34) The store has no concurrency test
- [#35](https://github.com/McCune1224/aegis/issues/35) One blocklist format applies to every list

## Decisions already made, so they do not get relitigated

`docs/stack.md` holds the tooling and why. `docs/design/filter-engine.md` holds
the engine shape, the client identity model, and why the order changed twice.

The ones a returning reader is most likely to question:

- **Solid 2 is a release candidate.** Pinned to `2.0.0-rc.8`. There is no mature
  Solid-native node graph library, so the graph is our own code over PixiJS. That
  is the intent, not a fallback.
- **The store is the source of truth, not a config file.** The YAML file this
  project originally planned is downgraded to a first-boot seed.
- **The command line is a bootstrap path.** Listen address, upstream, database
  path, log level, and a seed. The web app is the configuration surface.
- **One snapshot per query.** The rule set and the identity table are read
  together, so a reload cannot pair two generations. This is why `dns.Config`
  takes one `Decider`.
- **`SetMaxOpenConns(1)` needs revisiting** when the query log starts writing at
  query rate. It is justified in a comment today.
- **`quic-go` is deferred.** DNS over QUIC is one transport for a large
  dependency. It should wait for a reason.

## How to work in this repo

`AGENTS.md` holds the commands and the conventions. The short version:

```
make test        # 87 tests, about 1.4s with the race detector
make lint
make crossbuild  # proves all four release targets stay static
make gen         # regenerate queries after changing db/query
```

`docs/testing.md` names the five verification tiers. Most work stops at the pure
tests or at local sockets on an ephemeral port. Tier 3 is how per-client policy
is proven with no sudo and no VM, and it is where the two-device check lives.

Tier 5, real ARM hardware, has never run.
