-- name: ListAccess :many
SELECT kind, cidr FROM access ORDER BY kind, cidr;

-- name: DeleteAccess :exec
DELETE FROM access;

-- name: InsertAccess :exec
INSERT INTO access (kind, cidr) VALUES (?, ?);
