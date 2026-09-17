-- +goose Up

CREATE TABLE schedules (
    name     TEXT PRIMARY KEY,
    priority INTEGER NOT NULL,
    windows  TEXT NOT NULL
);

ALTER TABLE rules ADD COLUMN schedule TEXT NOT NULL DEFAULT '';
ALTER TABLE rules ADD COLUMN client TEXT NOT NULL DEFAULT '';

-- +goose Down

DROP TABLE schedules;
ALTER TABLE rules DROP COLUMN schedule;
ALTER TABLE rules DROP COLUMN client;
