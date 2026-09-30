-- +goose Up

-- The feed refresh deletes every row of one feed. The primary key orders by
-- (domain, feed), so that delete cannot seek; the index below orders by feed
-- first, and domain second so the same index answers the insert's conflict
-- check on (domain, feed) for rows already present in the batch.

CREATE INDEX threat_domains_feed_idx ON threat_domains (feed, domain);

-- +goose Down

DROP INDEX threat_domains_feed_idx;
