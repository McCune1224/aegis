-- name: ListRewrites :many
SELECT domain, target
FROM rewrites ORDER BY domain;

-- name: SaveRewrite :one
INSERT INTO rewrites (domain, target)
VALUES (?, ?)
ON CONFLICT (domain) DO UPDATE SET target = excluded.target
RETURNING domain;

-- name: DeleteRewrite :exec
DELETE FROM rewrites WHERE domain = ?;
