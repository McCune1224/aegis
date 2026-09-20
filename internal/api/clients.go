package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// clientRequest is one client as it arrives over HTTP. The name comes from the
// path, because a client's name is the key policy carries.
type clientRequest struct {
	Profile   string   `json:"profile"`
	Notes     string   `json:"notes"`
	Addresses []string `json:"addresses"`
	Prefixes  []string `json:"prefixes"`
}

// clientResponse is one client as it leaves over HTTP.
type clientResponse struct {
	Name      string   `json:"name"`
	Profile   string   `json:"profile"`
	Notes     string   `json:"notes"`
	Addresses []string `json:"addresses"`
	Prefixes  []string `json:"prefixes"`
}

func (r clientRequest) record(key filter.ClientKey) (store.Client, error) {
	record := store.Client{Key: key, Profile: filter.ProfileID(r.Profile), Notes: r.Notes}
	for i, raw := range r.Addresses {
		address, err := netip.ParseAddr(raw)
		if err != nil {
			return store.Client{}, fmt.Errorf("addresses[%d]: %w", i, err)
		}
		record.Addresses = append(record.Addresses, address)
	}
	for i, raw := range r.Prefixes {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return store.Client{}, fmt.Errorf("prefixes[%d]: %w", i, err)
		}
		record.Prefixes = append(record.Prefixes, prefix)
	}
	return record, nil
}

func clientResponseFrom(record store.Client) clientResponse {
	response := clientResponse{
		Name:      string(record.Key),
		Profile:   string(record.Profile),
		Notes:     record.Notes,
		Addresses: make([]string, 0, len(record.Addresses)),
		Prefixes:  make([]string, 0, len(record.Prefixes)),
	}
	for _, address := range record.Addresses {
		response.Addresses = append(response.Addresses, address.String())
	}
	for _, prefix := range record.Prefixes {
		response.Prefixes = append(response.Prefixes, prefix.String())
	}
	return response
}

func decodeClient(r *http.Request) (clientRequest, error) {
	var request clientRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return clientRequest{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return request, nil
}

func (s *Server) listClients(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	clients := make([]clientResponse, 0, len(cfg.Clients))
	for _, record := range cfg.Clients {
		clients = append(clients, clientResponseFrom(record))
	}
	writeJSON(w, http.StatusOK, clients)
}

func (s *Server) getClient(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	key := filter.ClientKey(r.PathValue("name"))
	for _, record := range cfg.Clients {
		if record.Key == key {
			writeJSON(w, http.StatusOK, clientResponseFrom(record))
			return
		}
	}
	writeError(w, errNotFound)
}

func (s *Server) putClient(w http.ResponseWriter, r *http.Request) {
	key := filter.ClientKey(r.PathValue("name"))
	request, err := decodeClient(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	record, err := request.record(key)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	err = s.apply(r.Context(),
		func(cfg *store.Config) error {
			for i := range cfg.Clients {
				if cfg.Clients[i].Key == key {
					cfg.Clients[i] = record
					return nil
				}
			}
			cfg.Clients = append(cfg.Clients, record)
			return nil
		},
		func(ctx context.Context) error { return s.store.SaveClient(ctx, record) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clientResponseFrom(record))
}

func (s *Server) deleteClient(w http.ResponseWriter, r *http.Request) {
	key := filter.ClientKey(r.PathValue("name"))
	err := s.apply(r.Context(),
		func(cfg *store.Config) error {
			for i := range cfg.Clients {
				if cfg.Clients[i].Key == key {
					for _, route := range cfg.Routes {
						if route.Client == string(key) {
							return conflict{fmt.Errorf("route %d still sends its queries to client %q", route.ID, key)}
						}
					}
					cfg.Clients = append(cfg.Clients[:i], cfg.Clients[i+1:]...)
					return nil
				}
			}
			return errNotFound
		},
		func(ctx context.Context) error { return s.store.DeleteClient(ctx, key) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
