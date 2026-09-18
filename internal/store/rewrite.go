package store

import (
	"context"
	"fmt"

	"aegis/internal/rewrite"
	"aegis/internal/store/storedb"
)

// SaveRewrite inserts one rewrite, replacing what the pattern held.
func (s *Store) SaveRewrite(ctx context.Context, record rewrite.Record) error {
	if _, err := s.queries.SaveRewrite(ctx, storedb.SaveRewriteParams{
		Domain: record.Pattern,
		Target: rewrite.TargetText(record),
	}); err != nil {
		return fmt.Errorf("store: save rewrite %s: %w", record.Pattern, err)
	}
	return nil
}

// Rewrites returns every rewrite, parsed into the typed form the engine and
// the handler take. A stored value that no longer parses fails the load,
// the same way a malformed profile does.
func (s *Store) Rewrites(ctx context.Context) ([]rewrite.Record, error) {
	rows, err := s.queries.ListRewrites(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: rewrites: %w", err)
	}
	records := make([]rewrite.Record, 0, len(rows))
	for _, row := range rows {
		record, err := rewrite.Parse(row.Domain, row.Target)
		if err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		records = append(records, record)
	}
	return records, nil
}

// DeleteRewrite removes one rewrite by its pattern.
func (s *Store) DeleteRewrite(ctx context.Context, pattern string) error {
	if err := s.queries.DeleteRewrite(ctx, pattern); err != nil {
		return fmt.Errorf("store: delete rewrite %s: %w", pattern, err)
	}
	return nil
}
