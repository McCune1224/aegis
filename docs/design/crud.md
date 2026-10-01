# One CRUD contract for every configuration list

Aegis has nine list pages that edit stored records. Before this record they
each hand-rolled their own edit, delete, and drawer wiring, so a page that
gained an edit button still had no duplicate, and a new page copied whichever
one its author opened first.

## The contract

Every list page offers five verbs over one record type.

    create     a New button opens a blank form
    read       the list and the row summaries
    update     Edit opens the same form with the stored record filled in
    delete     Delete on the row, one place only
    duplicate  Duplicate opens the same form as a copy under a free key

A page that offers one verb offers the others. `web/src/pages.test.tsx` renders
each page and fails if any row lacks edit, duplicate, or delete.

## Shapes

    Editor<K> =
      { mode: "closed" }
    | { mode: "new" }
    | { mode: "edit"; key: K }
    | { mode: "duplicate"; key: K }

`web/src/crud.ts` owns the state and the helpers.

- `openNew`, `openEdit`, `openDuplicate`, `closeEditor` build a state.
- `editorTitle` is the drawer heading. A string key already reads as the row
  name, so `Edit kids`. A numeric key would read as `Edit 7`, so it falls back
  to the noun and titles `Edit rule`.
- `duplicateName(name, taken)` returns `name-copy`, then `name-copy-2`, and on.
  It counts past a gap rather than reusing a live name.
- `createCrud<K>()` is the reactive form. It holds the editor state, a busy
  flag, and one error string, and its `run` wrapper clears busy and records the
  failure. A page cannot forget either.

`web/src/CrudActions.tsx` is the one row action row. It renders edit,
duplicate, and delete under the prefix the page passes, and stops propagation
so a row click does not fire behind it.

## Duplicate and the key

What a copy needs depends on what identifies the record.

- **Named.** Profiles, clients, sources, schedules, upstreams, and windows are
  keyed by a name the API writes through. The copy takes `duplicateName` and the
  name field stays editable.
- **Domain pattern.** A rewrite is keyed by its pattern. `example.com-copy` is
  not a name any resolver would match, so the pattern comes up empty and only
  the target carries over.
- **Numeric id.** Rules and routes are keyed by an id the database assigns. The
  copy is a create with the fields filled in. For a rule the domain comes up
  empty, since two rows for one domain would match the same queries.

In every case duplicate writes through the create or save handler the page
already had, so no page grew a new prop and `App.tsx` did not change.

## What this record also fixed

- `saveSource` in `web/src/api.ts` dropped `refresh_seconds`, so the hours box
  on a source did nothing. It now travels as a number, and a cleared box sends
  0, which `internal/runtime/sources.go` reads as the default interval.
- `Drawer` called `onCleanup` inside an effect body. In Solid 2 that has no
  owner, so it never ran and the Escape listener outlived the drawer. An effect
  returns its cleanup instead. `web/src/Drawer.test.tsx` proves the listener is
  gone after unmount.

## Rows that were buttons

Profiles and clients listed entries as `<button>` elements. An action row
cannot nest inside a button, so an entry is now a `div` with `role="button"`,
a `tabindex`, and Enter and Space handling. The action row stops propagation,
so clicking Edit does not also select the row.

## Not in this contract

Blocked services is a different shape on top of its window list: a scope picker
and per service sliders edit a whole set, not one record. The service toggles
keep their own apply and save indicator. The window list below it follows the
contract like every other list.
