-- name: GetSetting :one
SELECT value FROM settings WHERE key = ?;

-- name: SetSetting :exec
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: ListProfiles :many
SELECT name, extends, mode, custom FROM profiles ORDER BY name;

-- name: UpsertProfile :exec
INSERT INTO profiles (name, extends, mode, custom) VALUES (?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    extends = excluded.extends,
    mode = excluded.mode,
    custom = excluded.custom;

-- name: DeleteProfile :exec
DELETE FROM profiles WHERE name = ?;

-- name: ListClients :many
SELECT name, profile, notes FROM clients ORDER BY name;

-- name: UpsertClient :exec
INSERT INTO clients (name, profile, notes) VALUES (?, ?, ?)
ON CONFLICT (name) DO UPDATE SET profile = excluded.profile, notes = excluded.notes;

-- name: DeleteClient :exec
DELETE FROM clients WHERE name = ?;

-- name: ListClientAddresses :many
SELECT address, client FROM client_addresses ORDER BY address;

-- name: UpsertClientAddress :exec
INSERT INTO client_addresses (address, client) VALUES (?, ?)
ON CONFLICT (address) DO UPDATE SET client = excluded.client;

-- name: DeleteClientAddress :exec
DELETE FROM client_addresses WHERE address = ?;

-- name: DeleteClientAddressesForClient :exec
DELETE FROM client_addresses WHERE client = ?;

-- name: ListClientPrefixes :many
SELECT prefix, client FROM client_prefixes ORDER BY prefix;

-- name: UpsertClientPrefix :exec
INSERT INTO client_prefixes (prefix, client) VALUES (?, ?)
ON CONFLICT (prefix) DO UPDATE SET client = excluded.client;

-- name: DeleteClientPrefix :exec
DELETE FROM client_prefixes WHERE prefix = ?;

-- name: DeleteClientPrefixesForClient :exec
DELETE FROM client_prefixes WHERE client = ?;

-- name: ListClientMACs :many
SELECT mac, client FROM client_macs ORDER BY mac;

-- name: UpsertClientMAC :exec
INSERT INTO client_macs (mac, client) VALUES (?, ?)
ON CONFLICT (mac) DO UPDATE SET client = excluded.client;

-- name: DeleteClientMAC :exec
DELETE FROM client_macs WHERE mac = ?;

-- name: DeleteClientMACsForClient :exec
DELETE FROM client_macs WHERE client = ?;
