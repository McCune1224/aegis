-- +goose Up

CREATE TABLE sources (
    name       TEXT PRIMARY KEY,
    url        TEXT NOT NULL,
    format     TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    etag       TEXT NOT NULL DEFAULT '',
    last_fetch INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    rule_count INTEGER NOT NULL DEFAULT 0,
    body       BLOB NOT NULL DEFAULT ''
);

-- +goose Down

DROP TABLE sources;
