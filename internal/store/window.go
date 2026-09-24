package store

import (
	"context"
	"encoding/json"
	"fmt"

	"aegis/internal/filter"
	"aegis/internal/store/storedb"
)

// ServiceWindow is one time-scoped change to service blocking: while Schedule
// covers the query minute, every client in Clients has every service in
// Services blocked (Action Block) or exempted (Action Allow). Outside that
// minute the window contributes nothing. An allow window is how a service an
// always-on layer blocks becomes reachable for one client during one window.
type ServiceWindow struct {
	ID       int64
	Name     string
	Schedule string
	Action   filter.Action
	Clients  []filter.ClientKey
	Services []string
}

// ServiceWindows returns every window, ordered by name.
func (s *Store) ServiceWindows(ctx context.Context) ([]ServiceWindow, error) {
	rows, err := s.queries.ListServiceWindows(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: service windows: %w", err)
	}
	windows := make([]ServiceWindow, 0, len(rows))
	for _, row := range rows {
		window, err := serviceWindowFrom(row)
		if err != nil {
			return nil, err
		}
		windows = append(windows, window)
	}
	return windows, nil
}

// SaveServiceWindow inserts one window, replacing what its name held.
func (s *Store) SaveServiceWindow(ctx context.Context, window ServiceWindow) error {
	clients, err := json.Marshal(window.Clients)
	if err != nil {
		return fmt.Errorf("store: window %q: %w", window.Name, err)
	}
	services, err := json.Marshal(window.Services)
	if err != nil {
		return fmt.Errorf("store: window %q: %w", window.Name, err)
	}
	if _, err := s.queries.SaveServiceWindow(ctx, storedb.SaveServiceWindowParams{
		Name:     window.Name,
		Schedule: window.Schedule,
		Action:   window.Action.String(),
		Clients:  string(clients),
		Services: string(services),
	}); err != nil {
		return fmt.Errorf("store: save window %q: %w", window.Name, err)
	}
	return nil
}

// DeleteServiceWindow removes one window.
func (s *Store) DeleteServiceWindow(ctx context.Context, name string) error {
	if err := s.queries.DeleteServiceWindow(ctx, name); err != nil {
		return fmt.Errorf("store: delete window %q: %w", name, err)
	}
	return nil
}

func serviceWindowFrom(row storedb.ListServiceWindowsRow) (ServiceWindow, error) {
	var clients []filter.ClientKey
	if err := json.Unmarshal([]byte(row.Clients), &clients); err != nil {
		return ServiceWindow{}, fmt.Errorf("store: window %q: %w", row.Name, err)
	}
	var services []string
	if err := json.Unmarshal([]byte(row.Services), &services); err != nil {
		return ServiceWindow{}, fmt.Errorf("store: window %q: %w", row.Name, err)
	}
	action, err := filter.ParseAction(row.Action)
	if err != nil {
		return ServiceWindow{}, fmt.Errorf("store: window %q: %w", row.Name, err)
	}
	return ServiceWindow{
		ID:       row.ID,
		Name:     row.Name,
		Schedule: row.Schedule,
		Action:   action,
		Clients:  clients,
		Services: services,
	}, nil
}

// validateServiceWindows refuses a window that names an unknown action, or a
// schedule, client, or service the configuration does not hold, because any of
// them would fail every reload.
func validateServiceWindows(windows []ServiceWindow, schedules []filter.ScheduleSpec, clients []Client, catalog []BlockedService) error {
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
			return fmt.Errorf("store: a window has no name")
		}
		if seen[window.Name] {
			return fmt.Errorf("store: window %q is defined twice", window.Name)
		}
		seen[window.Name] = true

		if window.Action != filter.ActionBlock && window.Action != filter.ActionAllow {
			return fmt.Errorf("store: window %q has an action that is neither block nor allow", window.Name)
		}
		if window.Schedule == "" || !knownSchedule[window.Schedule] {
			return fmt.Errorf("store: window %q names schedule %q, which is not defined", window.Name, window.Schedule)
		}
		if len(window.Clients) == 0 {
			return fmt.Errorf("store: window %q names no client", window.Name)
		}
		for _, client := range window.Clients {
			if !knownClient[client] {
				return fmt.Errorf("store: window %q names client %q, which is not defined", window.Name, client)
			}
		}
		if len(window.Services) == 0 {
			return fmt.Errorf("store: window %q names no service", window.Name)
		}
		for _, service := range window.Services {
			if !knownService[service] {
				return fmt.Errorf("store: window %q names service %q, which is not in the catalog", window.Name, service)
			}
		}
	}
	return nil
}
