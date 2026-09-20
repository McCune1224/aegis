-- name: ListServices :many
SELECT id, name, group_name, rules, fetched_at FROM services ORDER BY id;

-- name: UpsertService :exec
INSERT INTO services (id, name, group_name, rules, fetched_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name,
    group_name = excluded.group_name,
    rules = excluded.rules,
    fetched_at = excluded.fetched_at;

-- name: ListProfileServices :many
SELECT profile, service FROM profile_services ORDER BY profile, service;

-- name: ListServicesForProfile :many
SELECT service FROM profile_services WHERE profile = ? ORDER BY service;

-- name: DeleteProfileServices :exec
DELETE FROM profile_services WHERE profile = ?;

-- name: InsertProfileService :exec
INSERT INTO profile_services (profile, service) VALUES (?, ?);
