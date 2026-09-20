package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"aegis/internal/store/storedb"
)

// ThreatFinding is one pattern the analyser detected, with the evidence an
// operator needs to judge it.
type ThreatFinding struct {
	Time     time.Time
	Client   string
	Kind     string
	Summary  string
	Evidence []string
}

// ThreatFindingLimit is the retention bound for findings. The log table grows
// with the network's traffic; findings grow with its misbehaviour.
const ThreatFindingLimit = 5000

// RecordThreatFindings writes one batch in a single transaction on the log
// handle, so a burst of findings costs one commit.
func (s *Store) RecordThreatFindings(ctx context.Context, findings []ThreatFinding) error {
	err := s.inLogTx(ctx, func(q *storedb.Queries) error {
		for _, finding := range findings {
			err := q.InsertThreatFinding(ctx, storedb.InsertThreatFindingParams{
				Time:     finding.Time.UnixMilli(),
				Client:   finding.Client,
				Kind:     finding.Kind,
				Summary:  finding.Summary,
				Evidence: strings.Join(finding.Evidence, "\n"),
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: record threat findings: %w", err)
	}
	return nil
}

// ThreatFindings reads the newest findings.
func (s *Store) ThreatFindings(ctx context.Context, limit int) ([]ThreatFinding, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.logQuer.ListThreatFindings(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("store: threat findings: %w", err)
	}
	findings := make([]ThreatFinding, 0, len(rows))
	for _, row := range rows {
		findings = append(findings, ThreatFinding{
			Time:     time.UnixMilli(row.Time),
			Client:   row.Client,
			Kind:     row.Kind,
			Summary:  row.Summary,
			Evidence: splitEvidence(row.Evidence),
		})
	}
	return findings, nil
}

// TrimThreatFindings keeps only the newest rows. It runs after a flush, the
// way the query log's trim does.
func (s *Store) TrimThreatFindings(ctx context.Context) error {
	err := s.logQuer.TrimThreatFindings(ctx, ThreatFindingLimit)
	if err != nil {
		return fmt.Errorf("store: trim threat findings: %w", err)
	}
	return nil
}

// ThreatFeed is one configured source of classified domains.
type ThreatFeed struct {
	Name    string
	URL     string
	Enabled bool
}

// ThreatDomain is one classified domain a feed carries.
type ThreatDomain struct {
	Domain string
	Kind   string
	Feed   string
}

// ThreatFeeds returns the configured feeds.
func (s *Store) ThreatFeeds(ctx context.Context) ([]ThreatFeed, error) {
	rows, err := s.queries.ListThreatFeeds(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: threat feeds: %w", err)
	}
	feeds := make([]ThreatFeed, 0, len(rows))
	for _, row := range rows {
		feeds = append(feeds, ThreatFeed{Name: row.Name, URL: row.Url, Enabled: row.Enabled == 1})
	}
	return feeds, nil
}

// SaveThreatFeed upserts one feed.
func (s *Store) SaveThreatFeed(ctx context.Context, feed ThreatFeed) error {
	enabled := 0
	if feed.Enabled {
		enabled = 1
	}
	err := s.queries.UpsertThreatFeed(ctx, storedb.UpsertThreatFeedParams{Name: feed.Name, Url: feed.URL, Enabled: int64(enabled)})
	if err != nil {
		return fmt.Errorf("store: save threat feed: %w", err)
	}
	return nil
}

// DeleteThreatFeed removes the feed and, through the foreign key, its domains.
func (s *Store) DeleteThreatFeed(ctx context.Context, name string) error {
	err := s.queries.DeleteThreatFeed(ctx, name)
	if err != nil {
		return fmt.Errorf("store: delete threat feed: %w", err)
	}
	return nil
}

// ReplaceThreatDomains writes what one feed just fetched over what it held, in
// one transaction so a reader never sees a half-updated feed.
func (s *Store) ReplaceThreatDomains(ctx context.Context, feed string, domains []ThreatDomain) error {
	err := s.inTx(ctx, func(q *storedb.Queries) error {
		if err := q.ReplaceThreatDomains(ctx, feed); err != nil {
			return err
		}
		for _, domain := range domains {
			err := q.InsertThreatDomain(ctx, storedb.InsertThreatDomainParams{
				Domain: domain.Domain,
				Kind:   domain.Kind,
				Feed:   feed,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: replace threat domains for %q: %w", feed, err)
	}
	return nil
}

// ThreatDomains returns every classified domain the feeds hold.
func (s *Store) ThreatDomains(ctx context.Context) ([]ThreatDomain, error) {
	rows, err := s.queries.ListThreatDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: threat domains: %w", err)
	}
	domains := make([]ThreatDomain, 0, len(rows))
	for _, row := range rows {
		domains = append(domains, ThreatDomain{Domain: row.Domain, Kind: row.Kind, Feed: row.Feed})
	}
	return domains, nil
}

func splitEvidence(joined string) []string {
	if joined == "" {
		return nil
	}
	return strings.Split(joined, "\n")
}
