-- +goose Up

CREATE TABLE rules (
    id      INTEGER PRIMARY KEY,
    domain  TEXT NOT NULL,
    kind    TEXT NOT NULL,
    action  TEXT NOT NULL,
    notes   TEXT NOT NULL DEFAULT '',
    created INTEGER NOT NULL DEFAULT 0
);

-- +goose Down

DROP TABLE rules;
