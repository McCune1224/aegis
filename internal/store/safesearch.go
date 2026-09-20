package store

import (
	"context"
	"fmt"

	"aegis/internal/filter"
	"aegis/internal/safesearch"
	"aegis/internal/store/storedb"
)

// ProfileSafesearch is one enablement: the profile enforces safe search for
// one engine.
type ProfileSafesearch struct {
	Profile filter.ProfileID
	Engine  safesearch.EngineID
}

// ProfileSafesearch returns every enablement, so a reload can rebuild the safe
// hosts each profile answers with.
func (s *Store) ProfileSafesearch(ctx context.Context) ([]ProfileSafesearch, error) {
	rows, err := s.queries.ListProfileSafesearch(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: profile safesearch: %w", err)
	}
	enables := make([]ProfileSafesearch, 0, len(rows))
	for _, row := range rows {
		enables = append(enables, ProfileSafesearch{Profile: filter.ProfileID(row.Profile), Engine: safesearch.EngineID(row.Engine)})
	}
	return enables, nil
}

// SetProfileSafesearch replaces the set of engines one profile enforces, so
// the stored set is always the whole answer to what that profile rewrites.
func (s *Store) SetProfileSafesearch(ctx context.Context, profile filter.ProfileID, engines []safesearch.EngineID) error {
	return s.inTx(ctx, func(q *storedb.Queries) error {
		if err := q.DeleteProfileSafesearch(ctx, string(profile)); err != nil {
			return fmt.Errorf("store: profile %q safesearch: %w", profile, err)
		}
		for _, engine := range engines {
			if err := q.InsertProfileSafesearch(ctx, storedb.InsertProfileSafesearchParams{
				Profile: string(profile),
				Engine:  string(engine),
			}); err != nil {
				return fmt.Errorf("store: profile %q engine %q: %w", profile, engine, err)
			}
		}
		return nil
	})
}

// validateProfileSafesearch refuses an enablement that names a profile or an
// engine the build does not hold, because it would fail every reload.
func validateProfileSafesearch(enables []ProfileSafesearch, profiles []filter.ProfileSpec) error {
	knownProfile := make(map[filter.ProfileID]bool, len(profiles))
	for _, spec := range profiles {
		knownProfile[spec.ID] = true
	}
	knownEngine := make(map[safesearch.EngineID]bool)
	for _, engine := range safesearch.Catalog() {
		knownEngine[engine.ID] = true
	}
	for _, enable := range enables {
		if !knownProfile[enable.Profile] {
			return fmt.Errorf("store: profile %q enforces safe search but is not defined", enable.Profile)
		}
		if !knownEngine[enable.Engine] {
			return fmt.Errorf("store: profile %q enforces safe search for engine %q, which is not in the catalog", enable.Profile, enable.Engine)
		}
	}
	return nil
}
