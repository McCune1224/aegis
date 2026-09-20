package store

import (
	"context"
	"fmt"

	"aegis/internal/filter"
	"aegis/internal/store/storedb"
)

// Route sends matching queries to one named upstream. An empty domain matches
// every name and an empty client matches every client, so a row fills in only
// the half it cares about.
type Route struct {
	ID       int64
	Domain   string
	Client   string
	Upstream string
}

// Routes returns every stored route.
func (s *Store) Routes(ctx context.Context) ([]Route, error) {
	rows, err := s.queries.ListRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: routes: %w", err)
	}
	routes := make([]Route, 0, len(rows))
	for _, row := range rows {
		routes = append(routes, Route{ID: row.ID, Domain: row.Domain, Client: row.Client, Upstream: row.Upstream})
	}
	return routes, nil
}

// SaveRoute inserts one route and returns it with its id. The upstream must
// exist; the foreign key refuses the write otherwise. The client and domain
// are not keyed in the schema, so a caller validates them against the rest of
// the configuration before saving.
func (s *Store) SaveRoute(ctx context.Context, route Route) (Route, error) {
	stored, err := normalizeRoute(route)
	if err != nil {
		return Route{}, err
	}
	id, err := s.queries.InsertRoute(ctx, storedb.InsertRouteParams{
		Domain:   stored.Domain,
		Client:   stored.Client,
		Upstream: stored.Upstream,
	})
	if err != nil {
		return Route{}, fmt.Errorf("store: save route: %w", err)
	}
	stored.ID = id
	return stored, nil
}

// UpdateRoute replaces one route's domain, client, and upstream, and reports
// whether the store held it.
func (s *Store) UpdateRoute(ctx context.Context, route Route) (bool, error) {
	stored, err := normalizeRoute(route)
	if err != nil {
		return false, err
	}
	updated, err := s.queries.UpdateRoute(ctx, storedb.UpdateRouteParams{
		Domain:   stored.Domain,
		Client:   stored.Client,
		Upstream: stored.Upstream,
		ID:       stored.ID,
	})
	if err != nil {
		return false, fmt.Errorf("store: update route %d: %w", stored.ID, err)
	}
	return updated > 0, nil
}

// DeleteRoute removes one route, and reports whether the store held it.
func (s *Store) DeleteRoute(ctx context.Context, id int64) (bool, error) {
	deleted, err := s.queries.DeleteRoute(ctx, id)
	if err != nil {
		return false, fmt.Errorf("store: delete route %d: %w", id, err)
	}
	return deleted > 0, nil
}

// normalizeRoute lowercases the domain through the same parser the DNS path
// uses, so a route matches the names queries actually carry.
func normalizeRoute(route Route) (Route, error) {
	if route.Domain == "" {
		return route, nil
	}
	domain, err := filter.ParseDomain(route.Domain)
	if err != nil {
		return Route{}, fmt.Errorf("store: route %d: %w", route.ID, err)
	}
	route.Domain = domain.String()
	return route, nil
}
