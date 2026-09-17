-- name: ListSchedules :many
SELECT name, priority, windows
FROM schedules ORDER BY name;

-- name: SaveSchedule :one
INSERT INTO schedules (name, priority, windows)
VALUES (?, ?, ?)
ON CONFLICT (name) DO UPDATE SET priority = excluded.priority, windows = excluded.windows
RETURNING name;

-- name: DeleteSchedule :exec
DELETE FROM schedules WHERE name = ?;
