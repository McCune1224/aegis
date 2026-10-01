package api

import (
	"fmt"
	"net/http"
	"time"

	"aegis/internal/store"
)

// observedRow is one seen device on the wire.
type observedRow struct {
	Client   string `json:"client"`
	Queries  int64  `json:"queries"`
	LastSeen int64  `json:"last_seen"`
	Claimed  bool   `json:"claimed"`
}

// listObservedClients serves the addresses the query log saw inside the
// window, busiest first, with whether a configured client claims them. The
// window defaults to a day and stays under a week.
func (s *Server) listObservedClients(w http.ResponseWriter, r *http.Request) {
	window := 24 * time.Hour
	if raw := r.URL.Query().Get("window"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			writeError(w, badRequest{fmt.Errorf("api: window %q: %w", raw, err)})
			return
		}
		if parsed <= 0 || parsed > 7*24*time.Hour {
			writeError(w, badRequest{fmt.Errorf("api: window must be between 1h and 168h")})
			return
		}
		window = parsed
	}

	observed, err := s.store.ObservedClients(r.Context(), store.ObservedRead{Since: time.Now().Add(-window).UnixMilli(), Limit: 50})
	if err != nil {
		writeError(w, err)
		return
	}
	rows := make([]observedRow, 0, len(observed))
	for _, entry := range observed {
		rows = append(rows, observedRow{Client: entry.Client, Queries: entry.Queries, LastSeen: entry.LastSeen.UnixMilli(), Claimed: entry.Claimed})
	}
	writeJSON(w, http.StatusOK, map[string][]observedRow{"observed": rows})
}
