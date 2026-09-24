-- +goose Up

CREATE TABLE client_services (
    client  TEXT NOT NULL REFERENCES clients (name) ON DELETE CASCADE,
    service TEXT NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    PRIMARY KEY (client, service)
);

-- +goose Down

DROP TABLE client_services;
