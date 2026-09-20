-- name: ListUpstreams :many
SELECT name, url, enabled, backup FROM upstreams ORDER BY name;

-- name: UpsertUpstream :exec
INSERT INTO upstreams (name, url, enabled, backup) VALUES (?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    url = excluded.url,
    enabled = excluded.enabled,
    backup = excluded.backup;

-- name: DeleteUpstream :exec
DELETE FROM upstreams WHERE name = ?;
