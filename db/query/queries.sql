-- name: InsertQueries :execresult
INSERT INTO queries (time, client, name, type, verdict, rule)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListQueries :many
SELECT id, time, client, name, type, verdict, rule
FROM queries
WHERE (@client = '' OR client = @client)
  AND (@verdict = '' OR verdict = @verdict)
  AND (@exact_name = '' OR name = @exact_name OR name LIKE @suffix_name)
ORDER BY id DESC
LIMIT @limit;

-- name: TrimQueries :exec
DELETE FROM queries WHERE id NOT IN (
    SELECT id FROM queries ORDER BY id DESC LIMIT @keep
);
