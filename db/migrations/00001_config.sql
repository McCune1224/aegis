-- +goose Up

CREATE TABLE profiles (
    name    TEXT PRIMARY KEY,
    extends TEXT REFERENCES profiles (name) ON DELETE RESTRICT,
    mode    TEXT,
    custom  TEXT
);

CREATE TABLE clients (
    name    TEXT PRIMARY KEY,
    profile TEXT NOT NULL REFERENCES profiles (name) ON DELETE RESTRICT,
    notes   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE client_addresses (
    address TEXT PRIMARY KEY,
    client  TEXT NOT NULL REFERENCES clients (name) ON DELETE CASCADE
);

CREATE TABLE client_prefixes (
    prefix TEXT PRIMARY KEY,
    client TEXT NOT NULL REFERENCES clients (name) ON DELETE CASCADE
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO profiles (name, mode) VALUES ('default', 'nxdomain');
INSERT INTO settings (key, value) VALUES ('default_profile', 'default');

-- +goose Down

DROP TABLE settings;
DROP TABLE client_prefixes;
DROP TABLE client_addresses;
DROP TABLE clients;
DROP TABLE profiles;
