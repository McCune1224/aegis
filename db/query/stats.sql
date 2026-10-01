-- name: StatsVerdictTotals :many
SELECT verdict, COUNT(*) AS count FROM queries
WHERE time >= ? AND NOT EXISTS (
    SELECT 1 FROM stats_ignored g
    WHERE queries.name = g.name OR queries.name LIKE '%.' || g.name
)
GROUP BY verdict;

-- name: StatsHourBuckets :many
SELECT (time / 3600000) * 3600000 AS bucket,
       verdict,
       COUNT(*) AS count
FROM queries
WHERE time >= ? AND NOT EXISTS (
    SELECT 1 FROM stats_ignored g
    WHERE queries.name = g.name OR queries.name LIKE '%.' || g.name
)
GROUP BY bucket, verdict;

-- name: StatsTopNames :many
SELECT name, COUNT(*) AS count FROM queries
WHERE time >= ? AND NOT EXISTS (
    SELECT 1 FROM stats_ignored g
    WHERE queries.name = g.name OR queries.name LIKE '%.' || g.name
)
GROUP BY name
ORDER BY count DESC, name ASC
LIMIT ?;

-- name: StatsTopBlockedNames :many
SELECT name, COUNT(*) AS count FROM queries
WHERE time >= ? AND verdict = 'block' AND NOT EXISTS (
    SELECT 1 FROM stats_ignored g
    WHERE queries.name = g.name OR queries.name LIKE '%.' || g.name
)
GROUP BY name
ORDER BY count DESC, name ASC
LIMIT ?;

-- name: StatsTopClients :many
SELECT client, COUNT(*) AS count FROM queries
WHERE time >= ? AND NOT EXISTS (
    SELECT 1 FROM stats_ignored g
    WHERE queries.name = g.name OR queries.name LIKE '%.' || g.name
)
GROUP BY client
ORDER BY count DESC, client ASC
LIMIT ?;

-- name: DeleteStatsWindow :exec
DELETE FROM queries WHERE time >= ?;

-- name: StatsIgnoredNames :many
SELECT name FROM stats_ignored ORDER BY name ASC;

-- name: InsertStatsIgnoredName :exec
INSERT INTO stats_ignored (name) VALUES (?)
ON CONFLICT DO NOTHING;

-- name: ClearStatsIgnored :exec
DELETE FROM stats_ignored;

-- name: TrimQueriesBefore :exec
DELETE FROM queries WHERE time < ?;
