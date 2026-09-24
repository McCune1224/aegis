package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// windowRequest is one service window as it arrives over HTTP. The name comes
// from the path, because the name is the key an operator edits by. An absent
// action is a block window, which is the focus-window behaviour.
type windowRequest struct {
	Action   string   `json:"action"`
	Schedule string   `json:"schedule"`
	Clients  []string `json:"clients"`
	Services []string `json:"services"`
}

// windowResponse is one service window as it leaves over HTTP.
type windowResponse struct {
	Name     string   `json:"name"`
	Action   string   `json:"action"`
	Schedule string   `json:"schedule"`
	Clients  []string `json:"clients"`
	Services []string `json:"services"`
}

func windowResponseFrom(window store.ServiceWindow) windowResponse {
	clients := make([]string, 0, len(window.Clients))
	for _, client := range window.Clients {
		clients = append(clients, string(client))
	}
	services := window.Services
	if services == nil {
		services = []string{}
	}
	return windowResponse{
		Name:     window.Name,
		Action:   window.Action.String(),
		Schedule: window.Schedule,
		Clients:  clients,
		Services: services,
	}
}

func (s *Server) listWindows(w http.ResponseWriter, r *http.Request) {
	windows, err := s.store.ServiceWindows(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]windowResponse, 0, len(windows))
	for _, window := range windows {
		response = append(response, windowResponseFrom(window))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) getWindow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	windows, err := s.store.ServiceWindows(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	for _, window := range windows {
		if window.Name == name {
			writeJSON(w, http.StatusOK, windowResponseFrom(window))
			return
		}
	}
	writeError(w, errNotFound)
}

// putWindow creates one window or replaces what its name held, so the clients
// and services it changes take effect on the next reload. The body is the
// whole window, not a patch.
func (s *Server) putWindow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, badRequest{fmt.Errorf("a window needs a name")})
		return
	}
	request, err := decodeJSON[windowRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	action := strings.TrimSpace(request.Action)
	if action == "" {
		action = filter.ActionBlock.String()
	}
	parsed, err := filter.ParseAction(action)
	if err != nil {
		writeError(w, badRequest{fmt.Errorf("window action: %w", err)})
		return
	}
	if parsed == filter.ActionRewrite {
		writeError(w, badRequest{fmt.Errorf("window action: %q is not a window verdict", action)})
		return
	}
	services, err := normalizeIDSet(request.Services, "service")
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	clients, err := normalizeClients(request.Clients)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	window := store.ServiceWindow{
		Name:     name,
		Schedule: strings.TrimSpace(request.Schedule),
		Action:   parsed,
		Clients:  clients,
		Services: services,
	}

	err = s.apply(r.Context(),
		func(cfg *store.Config) error {
			for i := range cfg.ServiceWindows {
				if cfg.ServiceWindows[i].Name == name {
					cfg.ServiceWindows[i] = window
					return nil
				}
			}
			cfg.ServiceWindows = append(cfg.ServiceWindows, window)
			return nil
		},
		func(ctx context.Context) error { return s.store.SaveServiceWindow(ctx, window) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, windowResponseFrom(window))
}

func (s *Server) deleteWindow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := s.apply(r.Context(),
		func(cfg *store.Config) error {
			for i := range cfg.ServiceWindows {
				if cfg.ServiceWindows[i].Name == name {
					cfg.ServiceWindows = append(cfg.ServiceWindows[:i], cfg.ServiceWindows[i+1:]...)
					return nil
				}
			}
			return errNotFound
		},
		func(ctx context.Context) error { return s.store.DeleteServiceWindow(ctx, name) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// normalizeClients trims, dedupes, and sorts the client keys a window names, so
// a saved window has one spelling and one order. What names the kind is only
// the error text.
func normalizeClients(raw []string) ([]filter.ClientKey, error) {
	set, err := normalizeIDSet(raw, "client")
	if err != nil {
		return nil, err
	}
	clients := make([]filter.ClientKey, 0, len(set))
	for _, client := range set {
		clients = append(clients, filter.ClientKey(client))
	}
	return clients, nil
}
