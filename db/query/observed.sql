-- name: ObservedClients :many
SELECT q.client AS client,
       COUNT(*) AS queries,
       CAST(MAX(q.time) AS INTEGER) AS last_seen,
       EXISTS (SELECT 1 FROM client_addresses a WHERE a.address = q.client) AS claimed
FROM queries q
WHERE q.time >= ?
GROUP BY q.client
ORDER BY queries DESC, client ASC
LIMIT ?;
