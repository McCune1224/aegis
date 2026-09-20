package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"aegis/internal/store"
)

// ThreatRefresher refreshes threat feeds on demand. The sync in the runtime
// implements it, and the API calls it after a feed write so a new feed is
// fetched without a restart.
type ThreatRefresher interface {
	RefreshFeeds(ctx context.Context) error
	RefreshFeed(ctx context.Context, name string) error
}

// threatFindingRow is one analyser finding on the wire. Evidence is the sample
// of names or intervals that earned the finding.
type threatFindingRow struct {
	Time     int64    `json:"time"`
	Client   string   `json:"client"`
	Kind     string   `json:"kind"`
	Summary  string   `json:"summary"`
	Evidence []string `json:"evidence"`
}

// listThreatFindings serves the newest findings for the dashboard. The limit
// query parameter caps the read; fifty covers a panel.
func (s *Server) listThreatFindings(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, badRequest{fmt.Errorf("api: limit must be a positive integer")})
			return
		}
		limit = parsed
	}
	findings, err := s.store.ThreatFindings(r.Context(), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	rows := make([]threatFindingRow, 0, len(findings))
	for _, finding := range findings {
		rows = append(rows, threatFindingRow{
			Time:     finding.Time.UTC().UnixMilli(),
			Client:   finding.Client,
			Kind:     finding.Kind,
			Summary:  finding.Summary,
			Evidence: finding.Evidence,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": rows})
}

// threatFeedRow is one configured feed on the wire.
type threatFeedRow struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// threatFeedRequest is one feed as it arrives. A nil enabled keeps what is
// stored on an update and means enabled on a create.
type threatFeedRequest struct {
	URL     string `json:"url"`
	Enabled *bool  `json:"enabled"`
}

func (s *Server) listThreatFeeds(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.store.ThreatFeeds(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	rows := make([]threatFeedRow, 0, len(feeds))
	for _, feed := range feeds {
		rows = append(rows, threatFeedRow{Name: feed.Name, URL: feed.URL, Enabled: feed.Enabled})
	}
	writeJSON(w, http.StatusOK, map[string]any{"feeds": rows})
}

func (s *Server) putThreatFeed(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	request, err := decodeJSON[threatFeedRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	if request.URL == "" {
		writeError(w, badRequest{fmt.Errorf("api: a threat feed needs a url")})
		return
	}
	existing, err := s.store.ThreatFeeds(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	enabled := true
	for _, feed := range existing {
		if feed.Name == name {
			enabled = feed.Enabled
		}
	}
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	if err := s.store.SaveThreatFeed(r.Context(), store.ThreatFeed{Name: name, URL: request.URL, Enabled: enabled}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, threatFeedRow{Name: name, URL: request.URL, Enabled: enabled})
}

func (s *Server) deleteThreatFeed(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteThreatFeed(r.Context(), r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refreshThreatFeed fetches one feed now and republishes the index, so a new
// classification reaches the log without a restart.
func (s *Server) refreshThreatFeed(w http.ResponseWriter, r *http.Request) {
	if s.threats == nil {
		writeError(w, errNotFound)
		return
	}
	name := r.PathValue("name")
	if err := s.threats.RefreshFeed(r.Context(), name); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed", "feed": name})
}
