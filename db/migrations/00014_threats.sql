-- +goose Up

CREATE TABLE threat_findings (
    id       INTEGER PRIMARY KEY,
    time     INTEGER NOT NULL,
    client   TEXT NOT NULL,
    kind     TEXT NOT NULL,
    summary  TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT ''
);

CREATE INDEX threat_findings_time_idx ON threat_findings (time);

CREATE TABLE threat_feeds (
    name    TEXT PRIMARY KEY,
    url     TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE threat_domains (
    domain TEXT NOT NULL,
    kind   TEXT NOT NULL,
    feed   TEXT NOT NULL REFERENCES threat_feeds (name) ON DELETE CASCADE,
    PRIMARY KEY (domain, feed)
);

-- +goose Down

DROP TABLE threat_domains;
DROP TABLE threat_feeds;
DROP TABLE threat_findings;
