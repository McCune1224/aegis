-- +goose Up

CREATE TABLE stats_ignored (
    name TEXT PRIMARY KEY
);

-- +goose Down

DROP TABLE stats_ignored;
