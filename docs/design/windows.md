# Service windows

Windows used to live on their own page as focus windows: a schedule, a set of
clients, and a set of services blocked while the schedule held. They are now
the time layer of blocked services, with one addition: a direction.

## The decision

A window is `{name, schedule, action, clients, services}`:

- **action block** adds blocks for its clients while its schedule covers the
  query minute. This is the old focus window, unchanged in behaviour.
- **action allow** exempts its services for its clients while the schedule
  holds. An allow rule is an ordinary `filter.RuleSpec` with
  `Action: ActionAllow`, and the allow tier beats every block for the same
  name, so a service an always-on layer blocks becomes reachable for exactly
  one client during exactly one window.

Both directions compile through `internal/services.Specs` with the window's
`Scope`, so verdicts, provenance, and the query log keep one code path. The
table was renamed `focus_windows` to `service_windows` in migration 00018 with
an `action` column defaulting to `block`, so every existing window keeps its
behaviour.

The page is merged: **Blocked Services** owns the always-on slider catalog and
the window list and form. A service set an operator can toggle and a window an
operator can schedule are two layers of one question, "when does this service
block?", and answering it on two screens made each half explain the other.

## Where Aegis diverges from AdGuard Home

AdGuard Home has no allow windows. Its per-client blocked-services override
replaces the global set for that client, so an exemption is a permanent edit
of that client's set. Aegis windows are additive and time-scoped: the
always-on layers stay intact, and the exemption exists only while its schedule
holds. An operator can therefore let one client watch a video between 20:00
and 21:00 without rewriting what that client blocks the rest of the week.

Windows name clients, not profiles, which matches where an exemption is
wanted: one device, one schedule. A profile-wide time window would compile the
same way (the profile scope exists in `services.Scope`); it is not built
because no operator has asked for one.
