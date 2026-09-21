# Focus windows

Aegis is a sinkhole for focus, but until now every restriction it could express
was either always on or attached to one hand-written rule. This records the
data shape that lets an operator say "these devices are blocked from social
networks and games on school nights," how it compiles, and where it diverges
from AdGuard Home.

## The decision

A focus window is a named row with three references:

    focus_windows(id, name, schedule, clients, services)

`schedule` names one schedule; `clients` is the set of client keys the window
applies to; `services` is the set of catalog service ids it blocks. While the
schedule covers the query minute, every named client is blocked from every
named service; at every other minute the window contributes nothing.

The engine already had the primitive. A `filter.RuleSpec` may carry a `Client`
and a `Schedule`, and `Decide` skips a scheduled rule whose client is not the
querier. What was missing was a way to configure one: a client-scoped scheduled
rule could only be written by hand, one rule at a time, and only for a name an
operator typed. A focus window is that same rule, generated in bulk from a
catalog service and one row an operator can edit.

At reload the runtime expands each window into one block rule per (client,
catalog rule) pair, carrying the schedule and the client:

    RuleSpec{Source: youtube, Domain: youtube.com, Action: Block,
             Client: "kids-ipad", Schedule: "school-nights"}

So a verdict from a focus window is an ordinary block verdict with ordinary
provenance, the query log names the service, and the engine answers it on the
same path as every other rule, at one minute-table lookup.

## Why the window owns the site set

The alternative was a schedule on each service toggle in a profile. It was
rejected because a toggle is one service for one profile: blocking four social
networks for two children would be eight edits, and nothing would name the
group of them. A window names the whole intent once. The cost is that a window
repeats the clients and services of another window when two windows share them,
which is a link an operator can see in the list rather than infer across two
screens.

## Additive outside the window

A focus window only ever adds blocking. A service the profile already blocks
always-on stays blocked outside every window; a service that only a window
names is unblocked outside it. The rule is one sentence and it survives
overlapping windows, because two windows covering the same minute both
contribute their own rules and the engine's tier and specificity comparison
resolves what they share.

The alternative, where ticking a service made it blocked only during a window,
was rejected because it silently changes the meaning of the profile toggles
that already exist. An operator who enabled "YouTube" on the kids profile today
expects it blocked now, not blocked only after they also create a window.

## The category is a UI grouping, not a second scope

The fetched catalog groups services (`social_network`, `games`, and so on). A
category toggle in the Focus screen selects or clears every service in the
group, and what is stored is the resulting set of service ids. The catalog is
fetched and can change between releases, so storing the resolved ids means a
window blocks exactly what the operator ticked and a service that appears next
week is not blocked by a category they chose last week. An operator who wants
the new service adds it.

## The duplicate client-scope guard is relaxed, not removed

`filter.Compile` rejects a rule that names a client and no schedule, because a
client-scoped always-on rule is just a profile with one member and the profile
is where it belongs. That guard still holds: a focus window always carries a
schedule. The runtime is the only new producer of client-scoped rules, and it
cannot produce one without a schedule because the window's schedule is a
required reference.

## Where Aegis diverges from AdGuard Home

**AdGuard Home has no per-client schedule.** Its schedules attach to rules and
to blocked services globally, and its per-client control is the blocked-services
set. Aegis keeps the profile as the unit of always-on policy and adds the
window as the unit of time-scoped policy, which is the superset the product
goals call for. The difference is that Aegis can time-scope one service for one
device without writing a regex and without a filter list per child.

**The window is a first-class row, not a list of rules.** AdGuard expresses the
same intent as several rules that share a schedule name. Aegis stores the
intent and generates the rules, so deleting a window cannot leave an orphan
rule behind and renaming a client updates every window that names it.

## Deferred, with the reason

Including focus windows in the export document. The document already omits
profile services and SafeSearch enablements, which are the same kind of
operator policy keyed on catalog data, so a focus window would be the first
such row to travel. Doing it for one and not the others would make the document
inconsistent. Revisit when the document grows a section for catalog-keyed
policy and moves all three at once.
