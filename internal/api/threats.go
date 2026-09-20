package api

import (
	"fmt"
	"net/http"
	"strconv"
)

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
