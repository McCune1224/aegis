package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// queryRow is a logged query on the wire. It is the log's boundary type, so the
// store's shapes do not leak into the JSON the dashboard parses.
type queryRow struct {
	Time    int64  `json:"time"`
	Client  string `json:"client"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Verdict string `json:"verdict"`
	Rule    string `json:"rule,omitempty"`
	Threat  string `json:"threat,omitempty"`
}

// listQueries serves the query log, newest first. Filters: client (an address),
// name (the name and its subdomains), verdict (allow or block), and limit.
func (s *Server) listQueries(w http.ResponseWriter, r *http.Request) {
	readFilter := store.QueryFilter{Client: r.URL.Query().Get("client")}
	if name := r.URL.Query().Get("name"); name != "" {
		readFilter.Name = name
	}
	if verdict := r.URL.Query().Get("verdict"); verdict != "" {
		action, err := filter.ParseAction(verdict)
		if err != nil {
			writeError(w, badRequest{fmt.Errorf("api: verdict %q: %w", verdict, err)})
			return
		}
		readFilter.Verdict = &action
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil || parsed <= 0 {
			writeError(w, badRequest{errors.New("api: limit must be a positive number")})
			return
		}
		readFilter.Limit = parsed
	}

	entries, err := s.store.Queries(r.Context(), readFilter)
	if err != nil {
		writeError(w, err)
		return
	}

	rows := make([]queryRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, queryRow{
			Time:    entry.Time.UnixMilli(),
			Client:  entry.Client.Unmap().String(),
			Name:    entry.Name.String(),
			Type:    entry.Type,
			Verdict: entry.Verdict.String(),
			Rule:    entry.Rule,
			Threat:  entry.Threat,
		})
	}
	writeJSON(w, http.StatusOK, map[string][]queryRow{"queries": rows})
}
