package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// focusRequest is one focus window as it arrives over HTTP. The name comes
// from the path, because the name is the key an operator edits by.
type focusRequest struct {
	Schedule string   `json:"schedule"`
	Clients  []string `json:"clients"`
	Services []string `json:"services"`
}

// focusResponse is one focus window as it leaves over HTTP.
type focusResponse struct {
	Name     string   `json:"name"`
	Schedule string   `json:"schedule"`
	Clients  []string `json:"clients"`
	Services []string `json:"services"`
}

func focusResponseFrom(window store.FocusWindow) focusResponse {
	clients := make([]string, 0, len(window.Clients))
	for _, client := range window.Clients {
		clients = append(clients, string(client))
	}
	services := window.Services
	if services == nil {
		services = []string{}
	}
	return focusResponse{Name: window.Name, Schedule: window.Schedule, Clients: clients, Services: services}
}

func (s *Server) listFocus(w http.ResponseWriter, r *http.Request) {
	windows, err := s.store.FocusWindows(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]focusResponse, 0, len(windows))
	for _, window := range windows {
		response = append(response, focusResponseFrom(window))
	}
	writeJSON(w, http.StatusOK, response)
}

// putFocus creates one focus window or replaces what its name held, so the
// clients it blocks change on the next reload. The body is the whole window,
// not a patch, so a client or service left out is dropped even when the
// catalog has moved on since the operator last looked.
func (s *Server) putFocus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, badRequest{fmt.Errorf("a focus window needs a name")})
		return
	}
	request, err := decodeJSON[focusRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
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
	window := store.FocusWindow{
		Name:     name,
		Schedule: strings.TrimSpace(request.Schedule),
		Clients:  clients,
		Services: services,
	}

	err = s.apply(r.Context(),
		func(cfg *store.Config) error {
			for i := range cfg.FocusWindows {
				if cfg.FocusWindows[i].Name == name {
					cfg.FocusWindows[i] = window
					return nil
				}
			}
			cfg.FocusWindows = append(cfg.FocusWindows, window)
			return nil
		},
		func(ctx context.Context) error { return s.store.SaveFocusWindow(ctx, window) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, focusResponseFrom(window))
}

func (s *Server) deleteFocus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := s.apply(r.Context(),
		func(cfg *store.Config) error {
			for i := range cfg.FocusWindows {
				if cfg.FocusWindows[i].Name == name {
					cfg.FocusWindows = append(cfg.FocusWindows[:i], cfg.FocusWindows[i+1:]...)
					return nil
				}
			}
			return errNotFound
		},
		func(ctx context.Context) error { return s.store.DeleteFocusWindow(ctx, name) },
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
