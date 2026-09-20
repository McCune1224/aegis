-- +goose Up

CREATE TABLE upstreams (
    name TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    backup INTEGER NOT NULL DEFAULT 0
);

-- +goose Down

DROP TABLE upstreams;
