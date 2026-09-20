package api

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"aegis/internal/client"
	"aegis/internal/store"
)

// leaseRow is one granted lease on the wire.
type leaseRow struct {
	Address  string `json:"address"`
	MAC      string `json:"mac"`
	Client   string `json:"client,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Expires  int64  `json:"expires"`
}

// discoveryRow is one device no client claims, which is the prompt the
// dashboard shows.
type discoveryRow struct {
	MAC      string `json:"mac"`
	Address  string `json:"address"`
	Hostname string `json:"hostname,omitempty"`
	First    int64  `json:"first"`
	Last     int64  `json:"last"`
}

func leaseRowFrom(lease store.Lease) leaseRow {
	return leaseRow{
		Address:  lease.Address.Unmap().String(),
		MAC:      client.NormalizeMAC(lease.MAC),
		Client:   string(lease.Client),
		Hostname: lease.Hostname,
		Expires:  lease.Expires.UnixMilli(),
	}
}

func discoveryRowFrom(discovery store.Discovery) discoveryRow {
	return discoveryRow{
		MAC:      client.NormalizeMAC(discovery.MAC),
		Address:  discovery.Address.Unmap().String(),
		Hostname: discovery.Hostname,
		First:    discovery.First.UnixMilli(),
		Last:     discovery.Last.UnixMilli(),
	}
}

// listLeases serves the addresses the DHCP server has handed out.
func (s *Server) listLeases(w http.ResponseWriter, r *http.Request) {
	leases, err := s.store.Leases(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	rows := make([]leaseRow, 0, len(leases))
	now := time.Now()
	for _, lease := range leases {
		if lease.Expires.Before(now) {
			continue
		}
		rows = append(rows, leaseRowFrom(lease))
	}
	writeJSON(w, http.StatusOK, map[string][]leaseRow{"leases": rows})
}

// listDiscoveries serves the devices no client record claims.
func (s *Server) listDiscoveries(w http.ResponseWriter, r *http.Request) {
	discoveries, err := s.store.Discoveries(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	rows := make([]discoveryRow, 0, len(discoveries))
	for _, discovery := range discoveries {
		rows = append(rows, discoveryRowFrom(discovery))
	}
	writeJSON(w, http.StatusOK, map[string][]discoveryRow{"discoveries": rows})
}

// deleteDiscovery answers a prompt with "not now": the device stays unknown and
// the same device prompting again will bring it back.
func (s *Server) deleteDiscovery(w http.ResponseWriter, r *http.Request) {
	hardware, err := net.ParseMAC(r.PathValue("mac"))
	if err != nil {
		writeError(w, badRequest{fmt.Errorf("api: mac: %w", err)})
		return
	}
	if err := s.store.DeleteDiscovery(r.Context(), hardware); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
