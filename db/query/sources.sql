-- name: ListSources :many
SELECT name, url, format, enabled, etag, last_fetch, last_error, rule_count, skipped, failures, refresh_seconds, body
FROM sources ORDER BY name;

-- name: ListEnabledSources :many
SELECT name, url, format, enabled, etag, last_fetch, last_error, rule_count, skipped, failures, refresh_seconds, body
FROM sources WHERE enabled = 1 ORDER BY name;

-- name: UpsertSource :exec
INSERT INTO sources (name, url, format, enabled, refresh_seconds) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    url = excluded.url,
    format = excluded.format,
    enabled = excluded.enabled,
    refresh_seconds = excluded.refresh_seconds;

-- name: DeleteSource :exec
DELETE FROM sources WHERE name = ?;

-- name: RecordSourceFetch :exec
UPDATE sources
SET etag = @etag, last_fetch = @last_fetch, last_error = @last_error, rule_count = @rule_count,
    skipped = @skipped, body = @body,
    failures = CASE WHEN @last_error = '' THEN 0 ELSE failures + 1 END
WHERE name = @name;
