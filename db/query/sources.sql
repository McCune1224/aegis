-- name: ListSources :many
SELECT name, url, format, enabled, etag, last_fetch, last_error, rule_count, body
FROM sources ORDER BY name;

-- name: ListEnabledSources :many
SELECT name, url, format, enabled, etag, last_fetch, last_error, rule_count, body
FROM sources WHERE enabled = 1 ORDER BY name;

-- name: UpsertSource :exec
INSERT INTO sources (name, url, format, enabled) VALUES (?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    url = excluded.url,
    format = excluded.format,
    enabled = excluded.enabled;

-- name: DeleteSource :exec
DELETE FROM sources WHERE name = ?;

-- name: RecordSourceFetch :exec
UPDATE sources
SET etag = ?, last_fetch = ?, last_error = ?, rule_count = ?, body = ?
WHERE name = ?;
