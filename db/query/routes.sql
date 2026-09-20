-- name: ListRoutes :many
SELECT id, domain, client, upstream FROM routes ORDER BY id;

-- name: InsertRoute :one
INSERT INTO routes (domain, client, upstream) VALUES (?, ?, ?)
RETURNING id;

-- name: UpdateRoute :exec
UPDATE routes
SET domain = ?, client = ?, upstream = ?
WHERE id = ?;

-- name: DeleteRoute :exec
DELETE FROM routes WHERE id = ?;
