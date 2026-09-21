package store

import (
	"context"
	"encoding/json"
	"fmt"

	"aegis/internal/filter"
	"aegis/internal/store/storedb"
)

// FocusWindow is one time-scoped restriction: while Schedule covers the query
// minute, every client in Clients is blocked from every service in Services.
// Outside that minute the window contributes nothing.
type FocusWindow struct {
	ID       int64
	Name     string
	Schedule string
	Clients  []filter.ClientKey
	Services []string
}

// FocusWindows returns every focus window, ordered by name.
func (s *Store) FocusWindows(ctx context.Context) ([]FocusWindow, error) {
	rows, err := s.queries.ListFocusWindows(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: focus windows: %w", err)
	}
	windows := make([]FocusWindow, 0, len(rows))
	for _, row := range rows {
		window, err := focusWindowFrom(row)
		if err != nil {
			return nil, err
		}
		windows = append(windows, window)
	}
	return windows, nil
}

// SaveFocusWindow inserts one focus window, replacing what its name held.
func (s *Store) SaveFocusWindow(ctx context.Context, window FocusWindow) error {
	clients, err := json.Marshal(window.Clients)
	if err != nil {
		return fmt.Errorf("store: focus window %q: %w", window.Name, err)
	}
	services, err := json.Marshal(window.Services)
	if err != nil {
		return fmt.Errorf("store: focus window %q: %w", window.Name, err)
	}
	if _, err := s.queries.SaveFocusWindow(ctx, storedb.SaveFocusWindowParams{
		Name:     window.Name,
		Schedule: window.Schedule,
		Clients:  string(clients),
		Services: string(services),
	}); err != nil {
		return fmt.Errorf("store: save focus window %q: %w", window.Name, err)
	}
	return nil
}

// DeleteFocusWindow removes one focus window.
func (s *Store) DeleteFocusWindow(ctx context.Context, name string) error {
	if err := s.queries.DeleteFocusWindow(ctx, name); err != nil {
		return fmt.Errorf("store: delete focus window %q: %w", name, err)
	}
	return nil
}

func focusWindowFrom(row storedb.FocusWindow) (FocusWindow, error) {
	var clients []filter.ClientKey
	if err := json.Unmarshal([]byte(row.Clients), &clients); err != nil {
		return FocusWindow{}, fmt.Errorf("store: focus window %q: %w", row.Name, err)
	}
	var services []string
	if err := json.Unmarshal([]byte(row.Services), &services); err != nil {
		return FocusWindow{}, fmt.Errorf("store: focus window %q: %w", row.Name, err)
	}
	return FocusWindow{
		ID:       row.ID,
		Name:     row.Name,
		Schedule: row.Schedule,
		Clients:  clients,
		Services: services,
	}, nil
}

// validateFocusWindows refuses a window that names a schedule, a client, or a
// service the configuration does not hold, because it would fail every reload.
func validateFocusWindows(windows []FocusWindow, schedules []filter.ScheduleSpec, clients []Client, catalog []BlockedService) error {
	knownSchedule := make(map[string]bool, len(schedules))
	for _, schedule := range schedules {
		knownSchedule[schedule.Name] = true
	}
	knownClient := make(map[filter.ClientKey]bool, len(clients))
	for _, record := range clients {
		knownClient[record.Key] = true
	}
	knownService := make(map[string]bool, len(catalog))
	for _, row := range catalog {
		knownService[row.ID] = true
	}

	seen := make(map[string]bool, len(windows))
	for _, window := range windows {
		if window.Name == "" {
			return fmt.Errorf("store: a focus window has no name")
		}
		if seen[window.Name] {
			return fmt.Errorf("store: focus window %q is defined twice", window.Name)
		}
		seen[window.Name] = true

		if window.Schedule == "" || !knownSchedule[window.Schedule] {
			return fmt.Errorf("store: focus window %q names schedule %q, which is not defined", window.Name, window.Schedule)
		}
		if len(window.Clients) == 0 {
			return fmt.Errorf("store: focus window %q names no client", window.Name)
		}
		for _, client := range window.Clients {
			if !knownClient[client] {
				return fmt.Errorf("store: focus window %q names client %q, which is not defined", window.Name, client)
			}
		}
		if len(window.Services) == 0 {
			return fmt.Errorf("store: focus window %q names no service", window.Name)
		}
		for _, service := range window.Services {
			if !knownService[service] {
				return fmt.Errorf("store: focus window %q names service %q, which is not in the catalog", window.Name, service)
			}
		}
	}
	return nil
}
