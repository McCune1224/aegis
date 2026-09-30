-- +goose Up

CREATE TABLE routes_new (
    id       INTEGER PRIMARY KEY,
    domain   TEXT NOT NULL DEFAULT '',
    client   TEXT NOT NULL DEFAULT '',
    upstream TEXT NOT NULL REFERENCES upstreams(name) ON DELETE RESTRICT
);

INSERT INTO routes_new SELECT id, domain, client, upstream FROM routes;

DROP TABLE routes;

ALTER TABLE routes_new RENAME TO routes;

-- +goose Down

CREATE TABLE routes_cascade (
    id       INTEGER PRIMARY KEY,
    domain   TEXT NOT NULL DEFAULT '',
    client   TEXT NOT NULL DEFAULT '',
    upstream TEXT NOT NULL REFERENCES upstreams(name) ON DELETE CASCADE
);

INSERT INTO routes_cascade SELECT id, domain, client, upstream FROM routes;

DROP TABLE routes;

ALTER TABLE routes_cascade RENAME TO routes;
