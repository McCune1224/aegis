-- name: ListRules :many
SELECT id, domain, kind, action, notes, created
FROM rules ORDER BY id;

-- name: InsertRule :one
INSERT INTO rules (domain, kind, action, notes, created)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateRule :exec
UPDATE rules
SET domain = ?, kind = ?, action = ?, notes = ?, created = ?
WHERE id = ?;

-- name: DeleteRule :exec
DELETE FROM rules WHERE id = ?;
