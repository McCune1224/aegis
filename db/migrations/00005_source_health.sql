-- +goose Up

ALTER TABLE sources ADD COLUMN skipped INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sources ADD COLUMN failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sources ADD COLUMN refresh_seconds INTEGER NOT NULL DEFAULT 0;

-- +goose Down

-- SQLite cannot DROP COLUMN before 3.35, so the down migration rebuilds the
-- table without the new columns.
CREATE TABLE sources_new (
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

INSERT INTO sources_new (name, url, format, enabled, etag, last_fetch, last_error, rule_count, body)
SELECT name, url, format, enabled, etag, last_fetch, last_error, rule_count, body FROM sources;

DROP TABLE sources;
ALTER TABLE sources_new RENAME TO sources;
