package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aegis/internal/store"
	"aegis/internal/upstream"
)

// upstreamRequest is one upstream as it arrives over HTTP.
type upstreamRequest struct {
	URL     *string `json:"url"`
	Enabled *bool   `json:"enabled"`
	Backup  bool    `json:"backup"`
}

// upstreamResponse is one upstream as it leaves over HTTP. Name is the row as
// written and URL is the canonical form the transports read from. The health
// fields are live observations and zero when the running pool holds no such
// resolver.
type upstreamResponse struct {
	Name      string  `json:"name"`
	URL       string  `json:"url"`
	Enabled   bool    `json:"enabled"`
	Backup    bool    `json:"backup"`
	LatencyMS float64 `json:"latency_ms"`
	Failures  int     `json:"failures"`
	Down      bool    `json:"down"`
}

func upstreamResponseFrom(row store.Upstream) upstreamResponse {
	return upstreamResponse{Name: row.Name, URL: row.URL, Enabled: row.Enabled, Backup: row.Backup}
}

func (s *Server) listUpstreams(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Upstreams(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	health := map[string]upstream.Stat{}
	if s.upstreams != nil {
		for _, stat := range s.upstreams.Stats() {
			health[stat.Name] = stat
		}
	}
	response := make([]upstreamResponse, 0, len(rows))
	for _, row := range rows {
		entry := upstreamResponseFrom(row)
		if stat, ok := health[row.Name]; ok {
			entry.LatencyMS = float64(stat.EWMA) / float64(time.Millisecond)
			entry.Failures = stat.Failures
			entry.Down = stat.Down
		}
		response = append(response, entry)
	}
	writeJSON(w, http.StatusOK, response)
}

// putUpstream creates one upstream or replaces what its name held, so the
// answers of a running server change on the next reload.
func (s *Server) putUpstream(w http.ResponseWriter, r *http.Request) {
	request, err := decodeJSON[upstreamRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, badRequest{errors.New("an upstream needs a name")})
		return
	}
	if request.URL == nil || strings.TrimSpace(*request.URL) == "" {
		writeError(w, badRequest{errors.New("an upstream needs a url")})
		return
	}
	if request.Enabled == nil {
		writeError(w, badRequest{errors.New("an upstream needs an enabled flag")})
		return
	}

	row := store.Upstream{Name: name, URL: *request.URL, Enabled: *request.Enabled, Backup: request.Backup}
	if _, err := upstream.Parse(row.URL); err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	rows, err := s.store.Upstreams(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	if !keepsAnEnabledUpstream(rows, row.Name, row) {
		writeError(w, badRequest{errors.New("at least one enabled upstream must remain")})
		return
	}
	if err := s.store.SaveUpstream(ctx, row); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, upstreamResponseFrom(row))
}

// deleteUpstream removes one upstream by its name. A route still sending
// queries there is a conflict the operator resolves first.
func (s *Server) deleteUpstream(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	rows, err := s.store.Upstreams(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	if !keepsAnEnabledUpstream(rows, name, store.Upstream{}) {
		writeError(w, badRequest{errors.New("at least one enabled upstream must remain")})
		return
	}
	routes, err := s.store.Routes(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	for _, route := range routes {
		if route.Upstream == name {
			writeError(w, conflict{fmt.Errorf("route %d still sends its queries to upstream %q", route.ID, name)})
			return
		}
	}
	if err := s.store.DeleteUpstream(ctx, name); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// keepsAnEnabledUpstream reads the configuration a write would leave behind:
// every stored row except the one named, plus its replacement, if any. A row
// with no replacement is a delete.
func keepsAnEnabledUpstream(rows []store.Upstream, name string, replacement store.Upstream) bool {
	keeps := replacement.Enabled
	for _, row := range rows {
		if row.Name != name && row.Enabled {
			keeps = true
		}
	}
	return keeps
}
