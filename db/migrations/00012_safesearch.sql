-- +goose Up

CREATE TABLE profile_safesearch (
    profile TEXT NOT NULL REFERENCES profiles (name) ON DELETE CASCADE,
    engine  TEXT NOT NULL,
    PRIMARY KEY (profile, engine)
);

-- +goose Down

DROP TABLE profile_safesearch;
