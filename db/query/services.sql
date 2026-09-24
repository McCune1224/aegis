-- name: ListServices :many
SELECT id, name, group_name, rules, icon_svg, fetched_at FROM services ORDER BY id;

-- name: UpsertService :exec
INSERT INTO services (id, name, group_name, rules, icon_svg, fetched_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name,
    group_name = excluded.group_name,
    rules = excluded.rules,
    icon_svg = excluded.icon_svg,
    fetched_at = excluded.fetched_at;

-- name: ListProfileServices :many
SELECT profile, service FROM profile_services ORDER BY profile, service;

-- name: ListServicesForProfile :many
SELECT service FROM profile_services WHERE profile = ? ORDER BY service;

-- name: DeleteProfileServices :exec
DELETE FROM profile_services WHERE profile = ?;

-- name: InsertProfileService :exec
INSERT INTO profile_services (profile, service) VALUES (?, ?);

-- name: ListClientServices :many
SELECT client, service FROM client_services ORDER BY client, service;

-- name: ListServicesForClient :many
SELECT service FROM client_services WHERE client = ? ORDER BY service;

-- name: DeleteClientServices :exec
DELETE FROM client_services WHERE client = ?;

-- name: InsertClientService :exec
INSERT INTO client_services (client, service) VALUES (?, ?);

-- name: ListServiceWindows :many
SELECT id, name, schedule, action, clients, services
FROM service_windows ORDER BY name;

-- name: ServiceWindowByName :one
SELECT id, name, schedule, action, clients, services
FROM service_windows WHERE name = ?;

-- name: SaveServiceWindow :one
INSERT INTO service_windows (name, schedule, action, clients, services)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    schedule = excluded.schedule,
    action   = excluded.action,
    clients  = excluded.clients,
    services = excluded.services
RETURNING id;

-- name: DeleteServiceWindow :exec
DELETE FROM service_windows WHERE name = ?;
