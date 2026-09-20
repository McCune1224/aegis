package store

import (
	"context"
	"fmt"

	"aegis/internal/store/storedb"
	"aegis/internal/upstream"
)

// Upstream is one stored resolver row. Name is the URL as the operator wrote
// it, for status and logs; URL is the canonical form the transports parse
// from, so the row reads the same no matter which alias saved it.
type Upstream struct {
	Name    string
	URL     string
	Enabled bool
	Backup  bool
}

// Upstreams returns every stored resolver row. The URL stays text here: the
// store holds what was saved, and the runtime owns the typed specs, so a row
// whose text no longer parses is named by the reload that reads it.
func (s *Store) Upstreams(ctx context.Context) ([]Upstream, error) {
	rows, err := s.queries.ListUpstreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: upstreams: %w", err)
	}
	upstreams := make([]Upstream, 0, len(rows))
	for _, row := range rows {
		upstreams = append(upstreams, Upstream{
			Name:    row.Name,
			URL:     row.Url,
			Enabled: row.Enabled != 0,
			Backup:  row.Backup != 0,
		})
	}
	return upstreams, nil
}

// SaveUpstream validates the URL and stores its canonical form.
func (s *Store) SaveUpstream(ctx context.Context, row Upstream) error {
	spec, err := upstream.Parse(row.URL)
	if err != nil {
		return fmt.Errorf("store: upstream %q: %w", row.Name, err)
	}
	enabled, backup := int64(0), int64(0)
	if row.Enabled {
		enabled = 1
	}
	if row.Backup {
		backup = 1
	}
	if err := s.queries.UpsertUpstream(ctx, storedb.UpsertUpstreamParams{
		Name:    row.Name,
		Url:     spec.URL.String(),
		Enabled: enabled,
		Backup:  backup,
	}); err != nil {
		return fmt.Errorf("store: save upstream %q: %w", row.Name, err)
	}
	return nil
}

// DeleteUpstream removes one upstream row. A caller validates the resulting
// configuration first, because the resolver needs at least one enabled row.
func (s *Store) DeleteUpstream(ctx context.Context, name string) error {
	if err := s.queries.DeleteUpstream(ctx, name); err != nil {
		return fmt.Errorf("store: delete upstream %q: %w", name, err)
	}
	return nil
}
