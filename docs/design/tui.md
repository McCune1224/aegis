# Operator screens

`aegis top` watches the live query stream and `aegis query tail` follows the
recorded log. Both are bubbletea models under a cobra command, and both have a
`--json` twin so a script never needs a terminal. This records the shape and
what it deliberately leaves out. It is the design record for #57.

## The decision

The screens are clients of a running daemon's HTTP API, not readers of the
database the daemon has open. `top` subscribes to
`GET /api/v1/stream/queries`, which is the daemon's own SSE hub in memory; the
log screen reads `GET /api/v1/queries`. Neither opens SQLite.

Three reasons, in the order they matter:

- The live stream only exists inside the daemon. `top` cannot be anything but a
  client of the process that is resolving queries.
- The API already owns the boundary types for both the stream and the log, so
  the screens parse one thing instead of re-deriving the store's shapes.
- A second process opening the same SQLite file works, but it is a second reader
  racing the writer for no benefit when the daemon is right there.

The two paths decode into one type, `tui.Query`, which is also what `--json`
prints. The stream's wire shape calls the client an `address` and the verdict an
`action`; the log's shape calls them `client` and `verdict` and timestamps in
milliseconds. That difference stays at the boundary, in the decoders, and
nothing above them knows about it.

## Layering

Cobra owns the command, the flags, and the exit code. Bubbletea owns the screen
only, which is the split `docs/stack.md` records. The models are plain structs
whose data arrives as messages, so every screen behaviour is a unit test that
calls `Update` and reads `View` without a terminal. The terminal itself is
checked by driving the built binary in a real one, per `docs/testing.md`.

The frame is a pure function, `frame(title, rows, width, height)`, and the model
is a thin adapter over it. That keeps the layout rules testable without
bubbletea's identity, and it is where the interesting decisions live:

- Rows are newest first. A sinkhole operator is watching what just happened.
- Columns drop from the right as the terminal narrows: the client goes first,
  then the verdict, and the name survives longest, because a row without its
  name says nothing about what was asked.
- The header counts every decision the screen has seen even after rows fall off
  the bottom, so a burst does not hide behind the window.

`list` holds the window and the totals and is shared by both screens; the
screens differ only in where the rows come from and what the title says.

## The non-interactive twins

`aegis top --json` writes one JSON object per decision as it arrives and ends
when the stream ends. `aegis query tail --json` writes the newest window once,
oldest first the way `tail` reads, and `--follow` keeps printing the ones that
arrive after them.

The follow loop finds its cursor in the re-read window by value, not by
timestamp, because two queries can share a millisecond and a row that has aged
out of the window must not be printed twice. A row that cannot be found stops
the loop for that tick; the alternative is either a duplicate or a gap, and a
duplicate breaks `sort` and `uniq` downstream. `--follow` without `--json` is an
error rather than a silent no-op, because the screen already follows.

## What the screens do not do

**No filters, no detail view, no editing.** `top` and `tail` show the stream and
the log. Filtering is the query log's job, and it already lives in the API; when
an operator needs it, the flag goes on `query tail` and the parameter already
exists. Editing rules from a TUI would need confirmation flows and a second
writer to the configuration, which is a different design problem.

**No `bubbles` table widget.** `docs/stack.md` lists `bubbles` for tables and
viewports. The row shape here is adaptive in a way a fixed-column table fights,
and the whole renderer is 60 lines of padding and clipping, so the dependency
was not earned. If a screen arrives that needs scrolling selection or a
viewport, `bubbles` is still the choice.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| HTTP client of the running daemon | read the API's stream and log | chosen |
| direct SQLite reader | open the config database and tail it | rejected, no live stream and a second writer's view of the file |
| a single screen with two modes | one model, a key to switch sources | rejected, the two sources have different lifetimes and the flags differ |
| `bubbles/table` for the rows | fixed columns with a viewport | rejected, the adaptive narrow layout is simpler than what the widget would need |
