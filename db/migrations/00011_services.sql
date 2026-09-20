-- +goose Up

CREATE TABLE services (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT '',
    rules      TEXT NOT NULL,
    fetched_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE profile_services (
    profile TEXT NOT NULL REFERENCES profiles (name) ON DELETE CASCADE,
    service TEXT NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    PRIMARY KEY (profile, service)
);

-- +goose Down

DROP TABLE profile_services;
DROP TABLE services;
