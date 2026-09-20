-- name: ListRules :many
SELECT id, domain, kind, action, notes, created, schedule, client
FROM rules ORDER BY id;

-- name: RuleByID :one
SELECT id, domain, kind, action, notes, created, schedule, client
FROM rules WHERE id = ?;

-- name: InsertRule :one
INSERT INTO rules (domain, kind, action, notes, created, schedule, client)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateRule :exec
UPDATE rules
SET domain = ?, kind = ?, action = ?, notes = ?, created = ?, schedule = ?, client = ?
WHERE id = ?;

-- name: DeleteRule :execrows
DELETE FROM rules WHERE id = ?;
