package store

import (
	"context"
	"fmt"
	"time"

	"aegis/internal/blocklist"
	"aegis/internal/store/storedb"
)

// Source is one blocklist URL Aegis fetches rules from. Body is the last
// successful response, kept so a restart while the URL is down still serves the
// list.
type Source struct {
	Name      string
	URL       string
	Format    blocklist.Format
	Enabled   bool
	ETag      string
	LastFetch time.Time
	LastError string
	RuleCount int
	Body      []byte
}

// Sources returns every configured source.
func (s *Store) Sources(ctx context.Context) ([]Source, error) {
	rows, err := s.queries.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: sources: %w", err)
	}
	return parseSources(rows)
}

// EnabledSources returns the sources a fetch runs for.
func (s *Store) EnabledSources(ctx context.Context) ([]Source, error) {
	rows, err := s.queries.ListEnabledSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: sources: %w", err)
	}
	return parseSources(rows)
}

func parseSources(rows []storedb.Source) ([]Source, error) {
	sources := make([]Source, 0, len(rows))
	for _, row := range rows {
		format, err := blocklist.ParseFormat(row.Format)
		if err != nil {
			return nil, fmt.Errorf("store: source %q: %w", row.Name, err)
		}
		sources = append(sources, Source{
			Name:      row.Name,
			URL:       row.Url,
			Format:    format,
			Enabled:   row.Enabled != 0,
			ETag:      row.Etag,
			LastFetch: unixSeconds(row.LastFetch),
			LastError: row.LastError,
			RuleCount: int(row.RuleCount),
			Body:      row.Body,
		})
	}
	return sources, nil
}

func unixSeconds(seconds int64) time.Time {
	if seconds == 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

// SaveSource inserts or replaces one source. It leaves the fetch record alone,
// because a config change should not discard the ETag that makes the next
// refresh conditional.
func (s *Store) SaveSource(ctx context.Context, source Source) error {
	enabled := int64(0)
	if source.Enabled {
		enabled = 1
	}
	if err := s.queries.UpsertSource(ctx, storedb.UpsertSourceParams{
		Name:    source.Name,
		Url:     source.URL,
		Format:  source.Format.String(),
		Enabled: enabled,
	}); err != nil {
		return fmt.Errorf("store: save source %q: %w", source.Name, err)
	}
	return nil
}

// DeleteSource removes one source.
func (s *Store) DeleteSource(ctx context.Context, name string) error {
	if err := s.queries.DeleteSource(ctx, name); err != nil {
		return fmt.Errorf("store: delete source %q: %w", name, err)
	}
	return nil
}

// RecordSourceFetch stores the outcome of one fetch, so health, the ETag, and
// the last good body survive a restart.
func (s *Store) RecordSourceFetch(ctx context.Context, name, etag string, fetched time.Time, fetchErr error, ruleCount int, body []byte) error {
	message := ""
	if fetchErr != nil {
		message = fetchErr.Error()
	}
	seconds := int64(0)
	if !fetched.IsZero() {
		seconds = fetched.Unix()
	}
	if body == nil {
		body = []byte{}
	}
	if err := s.queries.RecordSourceFetch(ctx, storedb.RecordSourceFetchParams{
		Etag:      etag,
		LastFetch: seconds,
		LastError: message,
		RuleCount: int64(ruleCount),
		Body:      body,
		Name:      name,
	}); err != nil {
		return fmt.Errorf("store: record source %q: %w", name, err)
	}
	return nil
}
