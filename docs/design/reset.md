# Full-wipe reset

This records what `POST /api/v1/reset` empties, what it deliberately keeps, and
why the two callers confirm the same way.

## The decision

One endpoint owns the wipe. The body carries the confirmation, so a stray POST
changes nothing: `{"confirm":"reset"}` is required, anything else is a 400 that
names the word. The store does the work in one transaction, then the handler
republishes the resolver in the same call, so the live filter drops the
enablements the wipe removed rather than serving a generation that still has
them. Running it again is a no-op with zero counts, not an error.

Five areas go, one count each in the response:

- **clients** — the records and, through the foreign keys, their addresses,
  prefixes, hardware addresses, and per-client service enablements.
- **discoveries** — the DHCP sightings nothing has claimed.
- **query log** — the rows, which is also what every stat is read from.
- **services** — the fetched catalog, and with it the profile and client
  enablements that name its rows.
- **settings** — the settings table and the access lists, which is where the
  Settings screen writes them.

Two things that name a wiped record go with it even though they are not named
in the list, because a configuration that still pointed at a wiped client or
service fails `Config.Validate` and that would refuse every later write:

- service windows, which name both clients and services;
- routes scoped to a client, which are the only routes a wiped client breaks.

Profiles, rules, rewrites, schedules, upstreams, sources, and threat feeds are
outside the wipe and survive. Schedules outlive the windows that named them,
because a custom rule may name the same schedule.

Two settings rows are written back inside the same transaction. The `seeded`
marker, or the next boot would re-seed the flag configuration and put the wiped
clients straight back. And `default_profile`, which `Config.Load` cannot start
without: it returns to the first-boot `default` while that profile exists, and
otherwise keeps the name the store held, which the surviving profiles still
define. Everything else in the settings table, including the stored query-log
window, is gone.

## The callers

The endpoint has two, and both confirm on the wire rather than in front of it:

- `aegis reset --yes` posts the same body the browser does. Without `--yes` the
  command refuses before it opens a connection, so a mistyped command cannot
  reach a server.
- The Settings page arms on the first press and wipes on the second, so one
  slip of the mouse is a label change and the Cancel button is still there.

The catalog is fetched data, so the wipe drops it with the services area; one
press on the Services page's Refresh catalog, or the next boot, brings it back
and a service can be saved and block again. The proof for that round trip is
`node scripts/e2e/drive.mjs prove`.
