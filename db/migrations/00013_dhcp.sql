-- +goose Up

CREATE TABLE client_macs (
    mac    TEXT PRIMARY KEY,
    client TEXT NOT NULL REFERENCES clients (name) ON DELETE CASCADE
);

-- A lease is an address a device holds for a while, not configuration: it
-- expires, and the table is swept. The client column is set from the identity
-- the lease carried when it was written, and cleared if that client is deleted,
-- so a lease outlives the record that claimed it.
CREATE TABLE leases (
    address  TEXT PRIMARY KEY,
    mac      TEXT NOT NULL,
    client   TEXT REFERENCES clients (name) ON DELETE SET NULL,
    hostname TEXT NOT NULL DEFAULT '',
    expires  INTEGER NOT NULL
);

CREATE INDEX leases_by_mac ON leases (mac);

-- A discovery is a device seen serving itself an address that no client record
-- claims, which is the prompt the dashboard shows. It persists until the
-- operator makes a client of it or dismisses it.
CREATE TABLE discoveries (
    mac      TEXT PRIMARY KEY,
    address  TEXT NOT NULL,
    hostname TEXT NOT NULL DEFAULT '',
    first    INTEGER NOT NULL,
    last     INTEGER NOT NULL
);

-- +goose Down

DROP TABLE discoveries;
DROP TABLE leases;
DROP TABLE client_macs;
