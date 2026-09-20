-- +goose Up

CREATE TABLE routes (
    id       INTEGER PRIMARY KEY,
    domain   TEXT NOT NULL DEFAULT '',
    client   TEXT NOT NULL DEFAULT '',
    upstream TEXT NOT NULL REFERENCES upstreams(name) ON DELETE CASCADE
);

-- +goose Down

DROP TABLE routes;
