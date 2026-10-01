# Stats: server-side aggregation over the query log

Aegis computes query statistics in SQL over the `queries` table instead of a
separate stats store. One store, one pruning rule, one read path.

## Why one store

AdGuard Home keeps statistics in a rotation file beside the query log, so
resetting stats and trimming the log are independent. Aegis keeps one SQLite
database and one `queries` table. The aggregates answer from that table, so:

- The stats retention interval is a second bound on the same table: the writer
  keeps rows inside the interval and at most `MaxRows` rows overall. Whichever
  binds first wins.
- `POST /api/v1/stats/reset` deletes the rows inside the window, which empties
  the log page for the same span. Recorded here rather than hidden.

## Shapes

    StatsConfig { Interval time.Duration }   // 0, 24h, 168h, 720h, 2160h; 0 = disabled
    StatsSummary {
        Total, Allowed, Blocked, Rewritten int64
        Series []HourBucket                // oldest first, zero-filled
        TopQueried, TopBlocked, TopClients []NameCount
    }

- `Interval` lives in the `settings` table as one JSON row. The enum matches
  AdGuard Home's 1/7/30/90-day choices; `0` disables.
- Ignored hosts live in a `stats_ignored` table, so the exclusion joins in SQL
  and a host is ignored by exact name or subdomain, the same name semantics as
  the log filter. Both sides pass `ParseDomain`, which lowercases, so the
  `LIKE '%.' || name` pattern needs no escaping: `%` and `_` cannot appear in
  a parsed domain.

## The endpoint

`GET /api/v1/stats?window=24h` returns the summary. The window is a multiple
of one hour, capped at the configured interval, defaulting to it. Buckets are
wall-clock hours, oldest first, zero-filled in Go from the grouped SQL result.

## Where Aegis differs from AdGuard Home

- No `avg_processing_time` and no per-upstream tops: the log stores neither a
  duration nor the answering upstream, and faking zeros would lie. A future
  column can add them honestly.
- No `replaced_safebrowsing` / `replaced_parental` counters: no such modules
  exist here yet. Aegis exposes rewritten instead.
- Disabling stats does not stop logging. The log page keeps working; only the
  time-based pruning and the endpoint's data change.
