package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"aegis/internal/store/storedb"
)

// Wipe is what one full reset gave up, one count per area it names.
type Wipe struct {
	Clients     int
	Discoveries int
	Queries     int
	Services    int
	Settings    int
}

// Reset empties the areas a full wipe names, in one transaction so a reader
// never sees half a wipe and a failed wipe changes nothing: the clients with
// their selectors, the discoveries, the query log, the services catalog with
// every enablement that names it, and the stored settings with the access
// lists.
//
// A window names clients and services and a route can name a client, so both
// go with them: a configuration that still named a wiped record would fail to
// validate, and that would refuse every write after the wipe. Profiles, rules,
// rewrites, schedules, upstreams, sources, and threat feeds are outside the
// wipe and stay.
//
// Two settings rows are written back. The seeded marker keeps the next boot
// from re-seeding the flag configuration over the wipe, and the default
// profile is one Config.Load cannot start without: it returns to the
// first-boot "default" while that profile exists, and otherwise keeps the name
// the store held, which the surviving profiles still define.
func (s *Store) Reset(ctx context.Context) (Wipe, error) {
	var wipe Wipe
	err := s.inTx(ctx, func(q *storedb.Queries) error {
		anchor, err := resetAnchor(ctx, q)
		if err != nil {
			return err
		}

		var settings, access int
		steps := []struct {
			name string
			into *int
			run  func() (sql.Result, error)
		}{
			{"service windows", nil, func() (sql.Result, error) { return q.WipeServiceWindows(ctx) }},
			{"client routes", nil, func() (sql.Result, error) { return q.WipeClientRoutes(ctx) }},
			{"clients", &wipe.Clients, func() (sql.Result, error) { return q.WipeClients(ctx) }},
			{"discoveries", &wipe.Discoveries, func() (sql.Result, error) { return q.WipeDiscoveries(ctx) }},
			{"queries", &wipe.Queries, func() (sql.Result, error) { return q.WipeQueries(ctx) }},
			{"services", &wipe.Services, func() (sql.Result, error) { return q.WipeServices(ctx) }},
			{"access", &access, func() (sql.Result, error) { return q.WipeAccess(ctx) }},
			{"settings", &settings, func() (sql.Result, error) { return q.WipeSettings(ctx) }},
		}
		for _, step := range steps {
			changed, err := rowsChanged(step.run())
			if err != nil {
				return fmt.Errorf("store: reset %s: %w", step.name, err)
			}
			if step.into != nil {
				*step.into = changed
			}
		}
		wipe.Settings = settings + access

		if err := q.SetSetting(ctx, storedb.SetSettingParams{Key: "seeded", Value: "1"}); err != nil {
			return fmt.Errorf("store: reset seeded marker: %w", err)
		}
		if err := q.SetSetting(ctx, storedb.SetSettingParams{Key: "default_profile", Value: anchor}); err != nil {
			return fmt.Errorf("store: reset default profile: %w", err)
		}
		return nil
	})
	if err != nil {
		return Wipe{}, err
	}
	return wipe, nil
}

// resetAnchor names the profile the wiped settings table comes back pointing
// at. The wipe never touches profiles, so the name the store held is still
// defined unless it was empty to begin with.
func resetAnchor(ctx context.Context, q *storedb.Queries) (string, error) {
	previous, err := q.GetSetting(ctx, "default_profile")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: reset default profile: %w", err)
	}

	profiles, err := q.ListProfiles(ctx)
	if err != nil {
		return "", fmt.Errorf("store: reset profiles: %w", err)
	}
	for _, profile := range profiles {
		if profile.Name == "default" {
			return "default", nil
		}
	}
	if previous == "" {
		return "", errors.New("store: reset: no default profile to point the wiped settings at")
	}
	return previous, nil
}

// rowsChanged counts what one statement changed, so a wipe can report the
// areas it emptied without reading them back.
func rowsChanged(result sql.Result, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(changed), nil
}
