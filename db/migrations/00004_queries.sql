-- +goose Up

CREATE TABLE queries (
    id      INTEGER PRIMARY KEY,
    time    INTEGER NOT NULL,
    client  TEXT NOT NULL,
    name    TEXT NOT NULL,
    type    TEXT NOT NULL,
    verdict TEXT NOT NULL,
    rule    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX queries_time_idx ON queries (time);
CREATE INDEX queries_name_idx ON queries (name);

-- +goose Down

DROP TABLE queries;
