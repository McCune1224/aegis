# Aegis stack

Decisions dated 2026-09-15. Module is `aegis`, Go 1.26.8.

## Why these choices

Aegis targets a single static binary that runs on a Raspberry Pi, a NAS, a router, and a laptop from the same source. That constraint drives most of what follows. Pure-Go dependencies keep `CGO_ENABLED=0` cross-compilation working for every target arch, so the release matrix stays a one-command build instead of a per-arch toolchain exercise.

## Backend

| Concern | Choice | Notes |
|---|---|---|
| HTTP router | stdlib `net/http` | Go 1.22 `ServeMux` carries method and path patterns, which is all the API surface needs. chi was the first choice, but once the standard library caught up it added a dependency without adding capability. |
| Live query stream | stdlib `net/http` with `text/event-stream` | Server-sent events. No upgrade handling, and `EventSource` reconnects on its own, which a dashboard wants. |
| DNS engine | `github.com/miekg/dns` | Message parsing, server, and client. The query pipeline is ours. |
| Upstream DoT | `miekg/dns` `Net: "tcp-tls"` | Built into the dns package. |
| Upstream DoH | stdlib `net/http` | POST and GET wire formats. |
| Upstream DoQ | `github.com/quic-go/quic-go` | The one transport miekg/dns does not cover. |
| Storage | `modernc.org/sqlite` | Pure Go. Registers driver name `sqlite`. No CGO. |
| Query codegen | `sqlc` | Build-time tool, not a runtime dependency. |
| Migrations | `github.com/pressly/goose/v3` | Used as a library over our own `*sql.DB`. |
| Config | `github.com/spf13/viper` | Flags over env over file over defaults. |
| CLI parsing | `github.com/spf13/cobra` | One dependency with viper for flag binding. |
| TUI | `charm.land/bubbletea/v2` | Interactive dashboard. Module path changed from `github.com/charmbracelet/bubbletea` in v2. |
| TUI layout | `charm.land/lipgloss/v2` | v2 adds compositing layers, which suits overlays and panels. |
| TUI widgets | `charm.land/bubbles/v2` | Tables, viewports, spinners. |
| Logging | `log/slog` | Stdlib. JSON in production, text in dev. |
| Metrics | `github.com/prometheus/client_golang` | `/metrics` endpoint. |
| Rate limiting | `golang.org/x/time/rate` | Per-client query limits. |
| IDNA | `golang.org/x/net/idna` | Punycode at the query boundary. |
| Tests | `github.com/stretchr/testify` | `require` for assertions. |

### Bubble Tea is not a CLI parser

Bubble Tea owns the interactive surface only. It has no notion of subcommands or flags, so the non-interactive surface needs cobra underneath it. The split is deliberate.

```
aegis serve                  # cobra, long-running daemon
aegis client add 10.0.0.5    # cobra, scriptable, exits
aegis blocklist update --all # cobra, scriptable, cron-able
aegis top                    # bubbletea, interactive, needs a TTY
aegis query tail             # bubbletea, interactive
```

Every interactive screen has a `--json` non-interactive twin. Scripts and CI never need a TTY.

### Integration detail for goose

Use goose as a library with the `*sql.DB` we open through modernc. Do not shell out to the goose CLI, because its bundled sqlite3 dialect expects the mattn CGO driver. Migrations are embedded with `//go:embed db/migrations/*.sql`.

### Caveat on sqlc and SQLite

sqlc's SQLite engine covers ordinary CRUD and filtering well. Its type inference and feature coverage trail the Postgres engine, and window functions or heavy aggregate queries may not generate cleanly. Code paths that fight the generator stay as hand-written `database/sql` in the same `internal/store` package rather than contorting the query to satisfy codegen.

## Frontend

| Concern | Choice | Notes |
|---|---|---|
| Framework | `solid-js@2.0.0-rc.8` | Pin exactly. Pre-release, API frozen at RC. |
| Build | Vite | With `@solidjs/vite-plugin`. |
| Router | `@solidjs/router` | Track the `next` dist-tag for Solid 2. |
| WebGL renderer | `pixi.js@8.x` | 8.16.0 current. WebGL and WebGPU, canvas fallback. |
| Graph layout | `elkjs` | Layered DAG layout, which matches a DNS pipeline. |
| Time series charts | `uplot` | Roughly 45KB, fast at high point counts. |
| Tests | `vitest` + `@solidjs/testing-library@next` | |
| E2E | `playwright` | The verification surface for the WebGL UI. |
| Types | `typescript` | Strict mode. |

### Solid 2 risk and mitigation

`solid-js@2.0.0-rc.8` is a release candidate, not a stable release. All runtime packages are ESM-only and require Node >= 22.12. Local Node is v22.23.1, so that is satisfied. Ancillary packages (`@solidjs/router`, `solid-primitives`, testing library) are mid-migration and may need `next` tags.

Exposure is limited because there is no mature Solid-native node graph library to depend on. The graph canvas is our own code over PixiJS, so the surface area touching Solid is signals, stores, and JSX. If RC churn becomes a tax before stable, dropping to Solid 1.9.x touches component code but not the renderer.

### Why we own the graph layer

React Flow has no Solid equivalent at parity. The graph is the product's differentiator, so owning it is the intent rather than a fallback. The Solid layer holds graph state in signals and stores. The PixiJS layer renders nodes, edges, and query-flow particles. Layout comes from elkjs. Interaction is translated from DOM pointer events into graph coordinates.

## Repository layout

```
aegis/
├── cmd/aegis/
│   └── main.go              # cobra root, wires config and commands
├── internal/
│   ├── config/              # viper loading, schema, validation
│   ├── dns/                 # pipeline, handlers, middleware chain
│   ├── upstream/            # resolver clients, health scoring, failover
│   ├── cache/               # response cache, TTL policy, prefetch
│   ├── filter/              # matching engine, rule evaluation
│   ├── blocklist/           # source registry, parsers, diffing, updates
│   ├── client/              # device identity, profiles, groups
│   ├── schedule/            # time windows, policy resolution
│   ├── analytics/           # aggregation, pattern detection
│   ├── threat/              # threat feed integration
│   ├── rewrite/             # local DNS records, PTR generation
│   ├── dhcp/                # DHCP server, lease tracking
│   ├── store/               # sqlc-generated queries, goose runner
│   ├── api/                 # chi routes, SSE hub, auth
│   └── tui/                 # bubbletea models, lipgloss styles
├── db/
│   ├── migrations/          # goose SQL files, embedded
│   ├── query/               # sqlc input SQL
│   └── sqlc.yaml
├── web/                     # Solid 2 app, embedded into the binary
├── docs/
├── Makefile
├── .goreleaser.yaml
└── go.mod
```

`internal/` for everything keeps the public surface at zero until there is a reason to expose an API for library consumers.

## Build and release

GoReleaser with `CGO_ENABLED=0` across linux/amd64, linux/arm64, linux/armv7, linux/armv6, darwin/arm64, darwin/amd64, and windows/amd64. Docker images built with `docker buildx` for the same linux arches, published multi-arch. The web bundle is built by Vite and embedded with `//go:embed web/dist`, so the shipped artifact is one file.

A `Makefile` owns the developer loop. GNU Make is present on every target host, so it adds no install step. `task` is not present and would be one more thing to install on a fresh machine and in CI.

```
make build     # go build -o bin/aegis ./cmd/aegis
make test      # go test -race ./...
make lint      # golangci-lint run
make fmt       # gofmt -l -w .
make vet       # go vet ./...
make tools     # go install goose, golangci-lint, goreleaser
```

`make build` gains a web bundle step when the Solid app exists.

## Verification path

Backend behavior is proven against a real resolver and a real SQLite file, not mocks. The DNS pipeline gets integration tests that run a `miekg/dns` server on an ephemeral port and assert on wire responses with literal expected values.

The TUI is verified with the `control-cli` skill: drive the built binary in a real terminal, capture frames, assert on rendered output for startup, navigation, and resize.

The web UI is verified with the `control-ui` skill: CDP against the Vite dev server and the embedded production build, for graph rendering, interaction, and layout at several viewport sizes. WebGL canvas output needs a screenshot diff rather than a DOM assertion, because the graph has no DOM to query.

## Open items

- Solid 2 stable release timing. Re-evaluate at each RC bump and at stable.
- Whether the DoQ transport earns its place in phase one or waits. It is a dependency on quic-go, which is large.
- Whether `uplot` covers the analytics charts or the dashboard needs stack-specific charts drawn in PixiJS for visual consistency.
