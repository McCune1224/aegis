-- name: ListLeases :many
SELECT address, mac, client, hostname, expires FROM leases ORDER BY address;

-- name: UpsertLease :exec
INSERT INTO leases (address, mac, client, hostname, expires) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (address) DO UPDATE SET
    mac = excluded.mac,
    client = excluded.client,
    hostname = excluded.hostname,
    expires = excluded.expires;

-- name: DeleteLease :exec
DELETE FROM leases WHERE address = ?;

-- name: DeleteExpiredLeases :execrows
DELETE FROM leases WHERE expires <= ?;

-- name: ListDiscoveries :many
SELECT mac, address, hostname, first, last FROM discoveries ORDER BY last DESC;

-- name: UpsertDiscovery :exec
INSERT INTO discoveries (mac, address, hostname, first, last) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (mac) DO UPDATE SET
    address = excluded.address,
    hostname = excluded.hostname,
    last = excluded.last;

-- name: DeleteDiscovery :exec
DELETE FROM discoveries WHERE mac = ?;
