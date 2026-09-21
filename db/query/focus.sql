-- name: ListFocusWindows :many
SELECT id, name, schedule, clients, services
FROM focus_windows ORDER BY name;

-- name: FocusWindowByName :one
SELECT id, name, schedule, clients, services
FROM focus_windows WHERE name = ?;

-- name: SaveFocusWindow :one
INSERT INTO focus_windows (name, schedule, clients, services)
VALUES (?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    schedule = excluded.schedule,
    clients  = excluded.clients,
    services = excluded.services
RETURNING id;

-- name: DeleteFocusWindow :exec
DELETE FROM focus_windows WHERE name = ?;
