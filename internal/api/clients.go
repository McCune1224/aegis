package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"

	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/store"
)

// clientRequest is one client as it arrives over HTTP. The name comes from the
// path, because a client's name is the key policy carries.
type clientRequest struct {
	Profile   string   `json:"profile"`
	Notes     string   `json:"notes"`
	Addresses []string `json:"addresses"`
	MACs      []string `json:"macs"`
	Prefixes  []string `json:"prefixes"`
}

// clientResponse is one client as it leaves over HTTP.
type clientResponse struct {
	Name      string   `json:"name"`
	Profile   string   `json:"profile"`
	Notes     string   `json:"notes"`
	Addresses []string `json:"addresses"`
	MACs      []string `json:"macs"`
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
	for i, raw := range r.MACs {
		hardware, err := net.ParseMAC(raw)
		if err != nil {
			return store.Client{}, fmt.Errorf("macs[%d]: %w", i, err)
		}
		record.MACs = append(record.MACs, hardware)
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
		MACs:      make([]string, 0, len(record.MACs)),
		Prefixes:  make([]string, 0, len(record.Prefixes)),
	}
	for _, address := range record.Addresses {
		response.Addresses = append(response.Addresses, address.String())
	}
	for _, hardware := range record.MACs {
		response.MACs = append(response.MACs, client.NormalizeMAC(hardware))
	}
	for _, prefix := range record.Prefixes {
		response.Prefixes = append(response.Prefixes, prefix.String())
	}
	return response
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
	request, err := decodeJSON[clientRequest](r)
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
					for _, window := range cfg.FocusWindows {
						for _, client := range window.Clients {
							if client == key {
								return conflict{fmt.Errorf("focus window %q still blocks client %q", window.Name, key)}
							}
						}
					}
					cfg.Clients = append(cfg.Clients[:i], cfg.Clients[i+1:]...)
					// The database cascades the enablements away; dropping them
					// here keeps the config the delete validates against
					// consistent with what it will become.
					enables := cfg.ClientServices[:0]
					for _, enable := range cfg.ClientServices {
						if enable.Client != key {
							enables = append(enables, enable)
						}
					}
					cfg.ClientServices = enables
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
