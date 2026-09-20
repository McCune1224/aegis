-- +goose Up

CREATE TABLE access (
    kind TEXT NOT NULL,
    cidr TEXT NOT NULL,
    PRIMARY KEY (kind, cidr)
);

-- +goose Down

DROP TABLE access;
