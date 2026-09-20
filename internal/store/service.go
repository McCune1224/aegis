package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"aegis/internal/filter"
	"aegis/internal/services"
	"aegis/internal/store/storedb"
)

// BlockedService is one row of the fetched services catalog: the rules that
// carry the service and the group it belongs to.
type BlockedService struct {
	ID        string
	Name      string
	Group     string
	Rules     []string
	FetchedAt time.Time
}

// ProfileService is one enablement: the profile blocks the service.
type ProfileService struct {
	Profile filter.ProfileID
	Service string
}

// Services returns the stored catalog.
func (s *Store) Services(ctx context.Context) ([]BlockedService, error) {
	rows, err := s.queries.ListServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: services: %w", err)
	}
	catalog := make([]BlockedService, 0, len(rows))
	for _, row := range rows {
		var rules []string
		if err := json.Unmarshal([]byte(row.Rules), &rules); err != nil {
			return nil, fmt.Errorf("store: service %q: %w", row.ID, err)
		}
		catalog = append(catalog, BlockedService{
			ID:        row.ID,
			Name:      row.Name,
			Group:     row.GroupName,
			Rules:     rules,
			FetchedAt: unixSeconds(row.FetchedAt),
		})
	}
	return catalog, nil
}

// SaveCatalog writes the catalog just fetched over the stored one, in one
// transaction so a reader never sees a half-updated catalog. A service the
// catalog no longer carries is left in place with its last rules, so an enabled
// service keeps blocking through a catalog that dropped it instead of silently
// unblocking; the same rule a failed blocklist fetch follows.
func (s *Store) SaveCatalog(ctx context.Context, fetched time.Time, catalog []services.Service) error {
	return s.inTx(ctx, func(q *storedb.Queries) error {
		for _, service := range catalog {
			rules, err := json.Marshal(service.Rules)
			if err != nil {
				return fmt.Errorf("store: service %q: %w", service.ID, err)
			}
			if err := q.UpsertService(ctx, storedb.UpsertServiceParams{
				ID:        service.ID,
				Name:      service.Name,
				GroupName: service.Group,
				Rules:     string(rules),
				FetchedAt: fetched.Unix(),
			}); err != nil {
				return fmt.Errorf("store: service %q: %w", service.ID, err)
			}
		}
		return nil
	})
}

// ProfileServices returns every enablement, so a reload can rebuild the rules
// each profile blocks.
func (s *Store) ProfileServices(ctx context.Context) ([]ProfileService, error) {
	rows, err := s.queries.ListProfileServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: profile services: %w", err)
	}
	enables := make([]ProfileService, 0, len(rows))
	for _, row := range rows {
		enables = append(enables, ProfileService{Profile: filter.ProfileID(row.Profile), Service: row.Service})
	}
	return enables, nil
}

// SetProfileServices replaces the set of services one profile blocks, so the
// stored set is always the whole answer to what that profile blocks.
func (s *Store) SetProfileServices(ctx context.Context, profile filter.ProfileID, serviceIDs []string) error {
	return s.inTx(ctx, func(q *storedb.Queries) error {
		if err := q.DeleteProfileServices(ctx, string(profile)); err != nil {
			return fmt.Errorf("store: profile %q services: %w", profile, err)
		}
		for _, id := range serviceIDs {
			if err := q.InsertProfileService(ctx, storedb.InsertProfileServiceParams{
				Profile: string(profile),
				Service: id,
			}); err != nil {
				return fmt.Errorf("store: profile %q service %q: %w", profile, id, err)
			}
		}
		return nil
	})
}

// validateProfileServices refuses an enablement that names a profile or a
// service the configuration does not hold, because it would fail every reload.
func validateProfileServices(enables []ProfileService, catalog []BlockedService, profiles []filter.ProfileSpec) error {
	knownProfile := make(map[filter.ProfileID]bool, len(profiles))
	for _, spec := range profiles {
		knownProfile[spec.ID] = true
	}
	knownService := make(map[string]bool, len(catalog))
	for _, row := range catalog {
		knownService[row.ID] = true
	}
	for _, enable := range enables {
		if !knownProfile[enable.Profile] {
			return fmt.Errorf("store: profile %q blocks services but is not defined", enable.Profile)
		}
		if !knownService[enable.Service] {
			return fmt.Errorf("store: profile %q blocks service %q, which is not in the catalog", enable.Profile, enable.Service)
		}
	}
	return nil
}
