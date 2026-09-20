package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// routeRequest is one route as it arrives over HTTP. An empty domain matches
// every name and an empty client matches every client.
type routeRequest struct {
	Domain   string `json:"domain"`
	Client   string `json:"client"`
	Upstream string `json:"upstream"`
}

// routeResponse is one route as it leaves over HTTP. Domain is the parsed,
// lowercased form the router matches against.
type routeResponse struct {
	ID       int64  `json:"id"`
	Domain   string `json:"domain"`
	Client   string `json:"client"`
	Upstream string `json:"upstream"`
}

func routeResponseFrom(row store.Route) routeResponse {
	return routeResponse{ID: row.ID, Domain: row.Domain, Client: row.Client, Upstream: row.Upstream}
}

func decodeRoute(r *http.Request) (routeRequest, error) {
	var request routeRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return routeRequest{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return request, nil
}

func (r routeRequest) row() (store.Route, error) {
	domain := ""
	if strings.TrimSpace(r.Domain) != "" {
		parsed, err := filter.ParseDomain(r.Domain)
		if err != nil {
			return store.Route{}, err
		}
		domain = parsed.String()
	}
	return store.Route{Domain: domain, Client: strings.TrimSpace(r.Client), Upstream: strings.TrimSpace(r.Upstream)}, nil
}

func routeID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errors.New("a route id must be a number")
	}
	return id, nil
}

func (s *Server) listRoutes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Routes(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]routeResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, routeResponseFrom(row))
	}
	writeJSON(w, http.StatusOK, response)
}

// postRoute stores one route, so the matching queries change upstreams on the
// next reload.
func (s *Server) postRoute(w http.ResponseWriter, r *http.Request) {
	request, err := decodeRoute(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	row, err := request.row()
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	if err := s.checkRoute(ctx, row); err != nil {
		writeError(w, err)
		return
	}
	stored, err := s.store.SaveRoute(ctx, row)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	writeJSON(w, http.StatusCreated, routeResponseFrom(stored))
}

// putRoute replaces one route's domain, client, and upstream.
func (s *Server) putRoute(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	request, err := decodeRoute(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	row, err := request.row()
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	row.ID = id

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	rows, err := s.store.Routes(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	if !slices.ContainsFunc(rows, func(existing store.Route) bool { return existing.ID == id }) {
		writeError(w, errNotFound)
		return
	}
	if err := s.checkRoute(ctx, row); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.UpdateRoute(ctx, row); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, routeResponseFrom(row))
}

// deleteRoute removes one route by its id.
func (s *Server) deleteRoute(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	rows, err := s.store.Routes(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	if !slices.ContainsFunc(rows, func(existing store.Route) bool { return existing.ID == id }) {
		writeError(w, errNotFound)
		return
	}
	if err := s.store.DeleteRoute(ctx, id); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkRoute refuses a route whose upstream is not an enabled resolver or
// whose client does not exist, the same rules a reload applies. The domain was
// parsed at decode, so the row already holds the form the router matches.
func (s *Server) checkRoute(ctx context.Context, row store.Route) error {
	cfg, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	enabled := false
	for _, upstream := range cfg.Upstreams {
		if upstream.Name == row.Upstream && upstream.Enabled {
			enabled = true
		}
	}
	if !enabled {
		return badRequest{fmt.Errorf("upstream %q is not an enabled resolver", row.Upstream)}
	}
	if row.Client == "" {
		return nil
	}
	for _, client := range cfg.Clients {
		if string(client.Key) == row.Client {
			return nil
		}
	}
	return badRequest{fmt.Errorf("client %q does not exist", row.Client)}
}
