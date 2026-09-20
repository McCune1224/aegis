# Editing policy as a graph

The constellation is where an operator sees the network; since #51 it is also
where they change it. This records what a gesture means, where the meanings
live, and why every write still goes through the endpoints the screens use. It
is the design record for #51 and the companion to `docs/design/graph-view.md`,
which owns the camera.

## The decision

A drag between two stars resolves to a typed intent before anything is written.
`web/src/edit.ts` owns the resolution: `linkIntent(nodes, from, to)` returns one
of three mutations, or nothing when the pair connects nothing.

| Drag | Intent | Endpoint that already owned the record |
| --- | --- | --- |
| client star → profile star | `reassign` | `PUT /api/v1/clients/{name}` |
| the unidentified star → profile star | `default` | `PUT /api/v1/default-profile` |
| profile star → the profile it should inherit from | `extends` | `PUT /api/v1/profiles/{name}` |
| anything → an upstream, star → itself, profile → client, rule → anything | none | no write |

Direction follows the edges: from the dependent node to the dependency. The
pairing table `LINKS` is one place that knows which kinds connect; the
unidentified node is a client kind but not a client record, so it is special-cased
by id and resolves to the default-policy write instead of a client update.

The renderer never decides what a gesture means. It draws the drag (the line is
the allow color when the pair connects, the block color when it does not, with a
ring on the drop target) and hands the resolved intent to the callback the app
wired. `applyLink` in `Graph.tsx` maps each intent onto the exact request body
the existing endpoints validate. The server stays the only validator: a cyclic
`extends` or an unknown profile is refused by the same compile check every
reload runs, and the refusal lands in an error line on the graph instead of a
partial write.

## The gesture state machine

One pointer sequence is one `Gesture`, a discriminated union in `Graph.tsx`:
idle, press (with the star under it, if any), pan, link, pinch. A press that
travels past `CLICK_TRAVEL` becomes a link when it started on a star and a pan
when it did not; a release under the threshold is a click and selects. The
loose locals it replaces (`moved`, `pinch`, an implicit is-this-a-drag) could
not say what a drag from a star was, which is the one thing this feature had to
define.

## Creating a profile

The create control writes a blank profile through `PUT /api/v1/profiles/{name}`,
the same upsert the Profiles screen uses. A name that already exists is refused
on the client, because the endpoint would silently overwrite it. After the
write, the next layout consumes a pending `reveal`: the new node is selected and
`ensureVisible` pans the least distance that brings it inside the viewport
margins, leaving the scale and the rest of the framing alone. The reveal is
claimed before the write so the redraw the refresh triggers cannot land between
the save and the claim.

## Rule nodes

Custom rules from #18 are part of the topology now: one small amber star per
rule, labeled with its domain, detailed with action, matcher, and client scope.
A rule scoped to a client draws a `scope` edge to that client's node; an
unscoped rule stands alone, because it applies to every client and no edge would
be honest about that. Rules are placement, not handles: the panel shows what the
rule is and sends the operator to the Rules screen to change it.

## The profile panel

Selecting a profile star keeps the panel it already had and adds the two fields
the issue names: blocking mode and, for `custom-address`, the address. Saving
sends the whole profile spec, so `extends` survives a mode edit. The mode
vocabulary is `BLOCKING_MODES` in `api.ts`, shared with the Profiles screen,
because the server's `filter.ParseBlockingMode` is the only definition and the
web now has one copy of it.

## Verification

The camera and gestures have a browser check over CDP against the built binary,
re-run after the gesture rewrite: pan still moves every star by the mouse delta,
zoom still grows the lit field, fit still returns to the first frame, a click
still selects, and a resize still keeps the camera.

| Step | Signal | Result |
| --- | --- | --- |
| pan by (180, 90) | centroid of the profile stars | moved by (180.0, 89.8) |
| wheel zoom | lit pixel count | grew 2030 → 4456 |
| fit | centroid | within 0.33px of the first frame |
| resize after a pan | star positions | within 0.01px |

The done-when from the issue, on the same harness: the browser edits the default
profile's mode to refused on the canvas, and a real DNS query reflects it
without a restart.

| Step | Signal | Result |
| --- | --- | --- |
| before the edit | `dig`-equivalent UDP query for the blocked name | rcode 3 (nxdomain) |
| edit mode on the canvas, save | the panel | answers refused |
| after the edit, no restart | same query | rcode 5 (refused) |
| allowed name after the edit | same query | rcode 0, untouched by the mode |

The connect and create check names each profile star by clicking it and reading
the panel the app opens, so a drag target is never guessed from pixels, then:

| Step | Signal | Result |
| --- | --- | --- |
| create `work` on the canvas | `GET /api/v1/profiles` | `work` exists; the panel opens on its node |
| drag unidentified → work | `GET /api/v1/default-profile` | `work` |
| drag work → default | `GET /api/v1/profiles/work` | `extends: "default"` |
| drag default → work | the graph error line | the server refused the cycle, the graph names it |
| drag a profile → the upstream star | `GET /api/v1/profiles` | byte-identical, no write |

## What is not built

**No editing or creating rules from the canvas.** The issue asked for rule
placement. Creating a rule from a drag needs a target picker for domain, kind,
and action, which is the Rules screen's form with extra steps.

**No disconnect gesture.** Unlinking a client from a profile means assigning it
somewhere else; unlinking an `extends` has no meaning the engine accepts, since
a blank parent is a mode-less profile with no chain. The Profiles and Clients
screens own those edits.

**No manual node positions.** ELK owns the layout, and the camera keeps the
operator's view of it. Dragging a star to a remembered position wants a
persistence story the graph does not have yet.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| intent table, renderer applies it | `linkIntent` returns a typed mutation | chosen |
| input state-machine module | pointer events fed to a separate controller that calls back | rejected, it hides thirty lines of bookkeeping behind five callbacks and organizes by event order instead of knowledge |
| server-side edge endpoint | the server interprets graph node ids | rejected, node ids are a view concept and the issue requires the existing endpoints |
