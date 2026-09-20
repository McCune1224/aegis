-- name: InsertThreatFinding :exec
INSERT INTO threat_findings (time, client, kind, summary, evidence)
VALUES (?, ?, ?, ?, ?);

-- name: ListThreatFindings :many
SELECT id, time, client, kind, summary, evidence
FROM threat_findings
ORDER BY id DESC
LIMIT @limit;

-- name: TrimThreatFindings :exec
DELETE FROM threat_findings WHERE id <= (
    SELECT id FROM threat_findings ORDER BY id DESC LIMIT 1 OFFSET @keep
);

-- name: ListThreatFeeds :many
SELECT name, url, enabled FROM threat_feeds ORDER BY name;

-- name: UpsertThreatFeed :exec
INSERT INTO threat_feeds (name, url, enabled)
VALUES (?, ?, ?)
ON CONFLICT (name) DO UPDATE SET url = excluded.url, enabled = excluded.enabled;

-- name: DeleteThreatFeed :exec
DELETE FROM threat_feeds WHERE name = ?;

-- name: ReplaceThreatDomains :exec
DELETE FROM threat_domains WHERE feed = ?;

-- name: InsertThreatDomain :exec
INSERT INTO threat_domains (domain, kind, feed)
VALUES (?, ?, ?)
ON CONFLICT (domain, feed) DO UPDATE SET kind = excluded.kind;

-- name: ListThreatDomains :many
SELECT domain, kind, feed FROM threat_domains;
