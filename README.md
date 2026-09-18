# aegis

A DNS sinkhole for focus. Blocks ads, trackers, and distracting sites for a
whole network, with per-client policy, blocklist management, schedules,
DNS rewrites, and a node graph dashboard. One static binary, pure Go, with an
embedded Solid web UI.

## Quickstart

Build it (the web bundle is built first and embedded):

    make tools   # once: installs goose, golangci-lint, goreleaser, sqlc
    make build

Run it:

    bin/aegis serve --dns-address 127.0.0.1:5354 --upstream 9.9.9.9:53

Open the UI at `http://127.0.0.1:8080` — profiles, clients, blocklist
sources, custom rules, and the live query stream are all there. Point a
device, or `dig`, at the DNS port:

    dig @127.0.0.1 -p 5354 doubleclick.net

The blocklist ships empty. Add a source in the UI (Sources tab) or at boot:

    bin/aegis serve --dns-address 127.0.0.1:5354 \
      --source stevenblack=https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts

Everything configured in the UI is stored in `aegis.db` next to where you ran
the binary and survives restarts.

### Caveats

- Port **5353 is mDNS** on many machines. For a trial, use a free high port
  like 5354 as above; for a real deployment, use 53 (or 5354 is fine too if
  your router forwards).
- **The HTTP API has no authentication.** Keep `--api-address` on loopback or
  a trusted management network; do not expose it to the internet.
- Encrypted serving is opt-in: pass `--dot-address`/`--doh-address` together
  with `--tls-cert`/`--tls-key` (operator-supplied PEM files). See
  `docs/design/tls-serving.md`.
- Prometheus-style counters live at `/metrics` on the API listener.

## Development

    make build   # build bin/aegis, running the web bundle first
    make dev     # Vite dev server against a local aegis
    make test    # Go tests with the race detector
    make lint    # golangci-lint (run make tools first)

- The web app lives in `web/` (Solid 2 over a WebGL canvas). `npm
  --prefix web run build` after editing web sources, or `make build` embeds a
  stale bundle.
- Database changes are goose migrations in `db/migrations`; queries are sqlc
  sources in `db/query` and regenerate with `make gen`. Never hand-edit
  `internal/store/storedb`.
- Docs under `docs/` record decisions, not status: `docs/stack.md` for why
  each dependency exists, `docs/testing.md` for the five verification tiers,
  `docs/design/` for per-feature records.

The tracker is the source of truth for what is done and what is next:
[#61 Product goals](https://github.com/McCune1224/aegis/issues/61) is the
index, and every open issue carries its own acceptance check. The target is a
feature clone of AdGuard Home first, then a superset.
