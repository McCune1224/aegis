-- +goose Up

CREATE TABLE rewrites (
    domain TEXT PRIMARY KEY,
    target TEXT NOT NULL
);

-- +goose Down

DROP TABLE rewrites;
