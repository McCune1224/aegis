-- name: ListSchedules :many
SELECT name, priority, windows
FROM schedules ORDER BY name;

-- name: SaveSchedule :one
INSERT INTO schedules (name, priority, windows)
VALUES (?, ?, ?)
ON CONFLICT (name) DO UPDATE SET priority = excluded.priority, windows = excluded.windows
RETURNING name;

-- name: ScheduleExists :one
SELECT EXISTS(SELECT 1 FROM schedules WHERE name = ?);

-- name: DeleteSchedule :exec
DELETE FROM schedules WHERE name = ?;
