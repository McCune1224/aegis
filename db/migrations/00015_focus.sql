-- +goose Up

CREATE TABLE focus_windows (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    name     TEXT NOT NULL UNIQUE,
    schedule TEXT NOT NULL,
    clients  TEXT NOT NULL,
    services TEXT NOT NULL
);

-- +goose Down

DROP TABLE focus_windows;
