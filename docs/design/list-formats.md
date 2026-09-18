# list formats

Blocklists ship in several shapes, and real filter sets mix them. This
records how Aegis names the format of each list.

## The decision

A list's format travels with the list, not with the process. Stored sources
hold a `format` column since the sources table landed, and the API edits it
per source; the `--blocklist` and `--source` flags accept an optional exact
prefix on each entry:

    --blocklist hosts:/etc/lists/hosts.txt
    --blocklist adblock:/etc/lists/rules.txt
    --source hagezi=adblock:https://example.com/list

A bare entry keeps the global `--block-format`, so existing invocations mean
what they meant before. The prefix is recognized only when it is exactly a
format name — `hosts`, `domains`, or `adblock` — so a Windows drive-letter
path (`C:\lists\x.txt`) or any other colon-bearing string is a path, not an
error. The parse happens at the flag boundary and the store receives the
typed `Format`, so nothing downstream re-checks it.

## AdGuard Home parity

AdGuard Home takes the opposite shape: one filtering engine reads every list,
trying each line as an AdGuard rule and then as a hosts entry, so a format is
not a property of a list at all. Aegis declares formats per list instead.
The difference is deliberate: a declared format parses a whole file with one
table (the line parser is indexed by format, `internal/blocklist`), keeps a
mixed `0.0.0.0 example.com` hosts line from being read as an allow-style
`@@` rule or vice versa, and makes list ingestion checks exact rather than
best-effort. The cost is that an operator converting a list to another shape
must rename its format — one word at the front of the entry.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| per-entry declared format | `[format:]path` prefix, global fallback | chosen |
| mixed-format engine | try every parser per line | rejected, it re-reads each line's shape on every rule rebuild and blurs which parser owns a malformed line |
| auto-detection | sniff the first lines of a file | rejected, a short or trimmed file detects as the wrong format and fails silently at query time |
