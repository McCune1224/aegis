package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"aegis/internal/blocklist"
	"aegis/internal/store/storedb"
)

// Source is one blocklist URL Aegis fetches rules from. Body is the last
// successful response, kept so a restart while the URL is down still serves the
// list.
type Source struct {
	Name           string
	URL            string
	Format         blocklist.Format
	Enabled        bool
	ETag           string
	LastFetch      time.Time
	LastError      string
	RuleCount      int
	Skipped        int
	Failures       int
	RefreshSeconds int
	Body           []byte
}

// Fresh reports whether the source was fetched within maxAge. A source that
// has never been fetched is not stale, it is new.
func (s Source) Fresh(now time.Time, maxAge time.Duration) bool {
	if s.LastFetch.IsZero() {
		return true
	}
	return now.Sub(s.LastFetch) < maxAge
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
	// The enabled and unfiltered queries return identical columns, so the rows
	// convert one to one.
	uniform := make([]storedb.ListSourcesRow, len(rows))
	for index, row := range rows {
		uniform[index] = storedb.ListSourcesRow(row)
	}
	return parseSources(uniform)
}

// SourceByName returns one configured source, and reports whether the store
// holds it.
func (s *Store) SourceByName(ctx context.Context, name string) (Source, bool, error) {
	row, err := s.queries.SourceByName(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return Source{}, false, nil
	}
	if err != nil {
		return Source{}, false, fmt.Errorf("store: source %q: %w", name, err)
	}
	// The single-row query returns the same columns as the list, so the row
	// converts one to one.
	source, err := parseSource(storedb.ListSourcesRow(row))
	if err != nil {
		return Source{}, false, err
	}
	return source, true, nil
}

func parseSources(rows []storedb.ListSourcesRow) ([]Source, error) {
	sources := make([]Source, 0, len(rows))
	for _, row := range rows {
		source, err := parseSource(row)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func parseSource(row storedb.ListSourcesRow) (Source, error) {
	format, err := blocklist.ParseFormat(row.Format)
	if err != nil {
		return Source{}, fmt.Errorf("store: source %q: %w", row.Name, err)
	}
	return Source{
		Name:           row.Name,
		URL:            row.Url,
		Format:         format,
		Enabled:        row.Enabled != 0,
		ETag:           row.Etag,
		LastFetch:      unixSeconds(row.LastFetch),
		LastError:      row.LastError,
		RuleCount:      int(row.RuleCount),
		Skipped:        int(row.Skipped),
		Failures:       int(row.Failures),
		RefreshSeconds: int(row.RefreshSeconds),
		Body:           row.Body,
	}, nil
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
		Name:           source.Name,
		Url:            source.URL,
		Format:         source.Format.String(),
		Enabled:        enabled,
		RefreshSeconds: int64(source.RefreshSeconds),
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
func (s *Store) RecordSourceFetch(ctx context.Context, name, etag string, fetched time.Time, fetchErr error, ruleCount, skipped int, body []byte) error {
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
		Skipped:   int64(skipped),
		Body:      body,
		Name:      name,
	}); err != nil {
		return fmt.Errorf("store: record source %q: %w", name, err)
	}
	return nil
}
