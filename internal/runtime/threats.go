package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"aegis/internal/blocklist"
	"aegis/internal/store"
	"aegis/internal/threat"
)

// IndexPublisher receives the classified domains every feed refresh produced,
// so the log enrichment reads one immutable index.
type IndexPublisher interface {
	PublishIndex(domains map[string]string)
}

// ThreatRefresher refreshes threat feeds on demand. The API calls it after a
// feed write, the way it refreshes blocklist sources.
type ThreatRefresher interface {
	RefreshFeeds(ctx context.Context) error
	RefreshFeed(ctx context.Context, name string) error
}

// ThreatSync fetches the enabled threat feeds, stores their classified
// domains, and republishes the lookup index the log enrichment reads.
type ThreatSync struct {
	database *store.Store
	fetcher  *blocklist.Fetcher
	index    IndexPublisher
	logger   *slog.Logger
}

// NewThreatSync returns a sync writing through the store and publishing to
// the index.
func NewThreatSync(database *store.Store, fetcher *blocklist.Fetcher, index IndexPublisher, logger *slog.Logger) *ThreatSync {
	if logger == nil {
		logger = slog.Default()
	}
	return &ThreatSync{database: database, fetcher: fetcher, index: index, logger: logger}
}

// RefreshFeeds fetches every enabled feed and republishes the index once.
func (s *ThreatSync) RefreshFeeds(ctx context.Context) error {
	feeds, err := s.database.ThreatFeeds(ctx)
	if err != nil {
		return err
	}
	changed := false
	for _, feed := range feeds {
		if feed.Enabled {
			if err := s.refreshFeed(ctx, feed); err != nil {
				return err
			}
			changed = true
		}
	}
	if changed {
		return s.publish(ctx)
	}
	return nil
}

// RefreshFeed fetches one feed by name and republishes the index.
func (s *ThreatSync) RefreshFeed(ctx context.Context, name string) error {
	feeds, err := s.database.ThreatFeeds(ctx)
	if err != nil {
		return err
	}
	for _, feed := range feeds {
		if feed.Name == name {
			if err := s.refreshFeed(ctx, feed); err != nil {
				return err
			}
			return s.publish(ctx)
		}
	}
	return fmt.Errorf("runtime: threat feed %q does not exist", name)
}

func (s *ThreatSync) refreshFeed(ctx context.Context, feed store.ThreatFeed) error {
	fetched, err := s.fetcher.Fetch(ctx, feed.URL, "")
	if err != nil {
		s.logger.Warn("threat feed failed", "feed", feed.Name, "url", feed.URL, "error", err)
		return nil
	}
	entries, err := threat.ParseFeed(fetched.Body)
	if err != nil {
		s.logger.Warn("threat feed did not parse", "feed", feed.Name, "url", feed.URL, "error", err)
		return nil
	}
	domains := make([]store.ThreatDomain, 0, len(entries))
	for _, entry := range entries {
		domains = append(domains, store.ThreatDomain{Domain: entry.Domain.String(), Kind: entry.Kind, Feed: feed.Name})
	}
	if err := s.database.ReplaceThreatDomains(ctx, feed.Name, domains); err != nil {
		return err
	}
	s.logger.Info("threat feed loaded", "feed", feed.Name, "domains", len(domains))
	return nil
}

// publish rebuilds the index from every stored domain and hands it to the
// publisher.
func (s *ThreatSync) publish(ctx context.Context) error {
	rows, err := s.database.ThreatDomains(ctx)
	if err != nil {
		return err
	}
	lookup := make(map[string]string, len(rows))
	for _, row := range rows {
		lookup[row.Domain] = row.Kind
	}
	s.index.PublishIndex(lookup)
	return nil
}
