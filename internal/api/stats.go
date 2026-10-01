package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aegis/internal/store"
)

// statsConfigRequest is the retention as it arrives: a duration string the
// operator can read. Zero disables the summary and the time-based pruning.
type statsConfigRequest struct {
	Interval *string  `json:"interval"`
	Ignored  []string `json:"ignored"`
}

// statsConfigResponse is the retention as it leaves, interval in hours so the
// number stays comparable across clients.
type statsConfigResponse struct {
	Interval string   `json:"interval"`
	Ignored  []string `json:"ignored"`
}

// statsIntervals are the retentions an operator may choose, AdGuard Home's
// one, seven, thirty, and ninety days, plus disabled.
var statsIntervals = map[string]time.Duration{
	"0":     0,
	"0h":    0,
	"24h":   24 * time.Hour,
	"168h":  7 * 24 * time.Hour,
	"7d":    7 * 24 * time.Hour,
	"720h":  30 * 24 * time.Hour,
	"30d":   30 * 24 * time.Hour,
	"2160h": 90 * 24 * time.Hour,
	"90d":   90 * 24 * time.Hour,
}

// hoursString renders a retention the operator chose: whole hours, so 24h
// reads "24h" and disabled reads "0h" instead of Go's "24h0m0s" and "0s".
func hoursString(d time.Duration) string {
	return fmt.Sprintf("%gh", d.Hours())
}

func (s *Server) getStatsConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.StatsConfig(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	ignored, err := s.statsIgnoredOrEmpty(r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statsConfigResponse{Interval: hoursString(config.Interval), Ignored: ignored})
}

// putStatsConfig replaces the whole retention: interval, and the ignored
// hosts when the body names them.
func (s *Server) putStatsConfig(w http.ResponseWriter, r *http.Request) {
	request, err := decodeJSON[statsConfigRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	if request.Interval == nil {
		writeError(w, badRequest{errors.New("api: a stats config needs an interval")})
		return
	}

	interval, ok := statsIntervals[strings.ToLower(*request.Interval)]
	if !ok {
		writeError(w, badRequest{fmt.Errorf("api: interval %q is not one of 0, 24h, 7d, 30d, 90d", *request.Interval)})
		return
	}

	config := store.StatsConfig{Interval: interval}
	if err := s.store.SaveStatsConfig(r.Context(), config); err != nil {
		writeError(w, err)
		return
	}
	if request.Ignored != nil {
		if err := s.store.SetStatsIgnored(r.Context(), request.Ignored); err != nil {
			writeError(w, err)
			return
		}
	}
	ignored, err := s.statsIgnoredOrEmpty(r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statsConfigResponse{Interval: hoursString(config.Interval), Ignored: ignored})
}

func (s *Server) statsIgnoredOrEmpty(r *http.Request) ([]string, error) {
	ignored, err := s.store.StatsIgnored(r.Context())
	if ignored == nil {
		ignored = []string{}
	}
	return ignored, err
}

// statsSeriesRow is one hour of the series on the wire.
type statsSeriesRow struct {
	Start     int64 `json:"start"`
	Total     int64 `json:"total"`
	Blocked   int64 `json:"blocked"`
	Rewritten int64 `json:"rewritten"`
}

type statsNameRow struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type statsClientRow struct {
	Client string `json:"client"`
	Count  int64  `json:"count"`
}

type statsResponse struct {
	Window     int64            `json:"window"`
	Total      int64            `json:"total"`
	Allowed    int64            `json:"allowed"`
	Blocked    int64            `json:"blocked"`
	Rewritten  int64            `json:"rewritten"`
	Series     []statsSeriesRow `json:"series"`
	TopQueried []statsNameRow   `json:"top_queried"`
	TopBlocked []statsNameRow   `json:"top_blocked"`
	TopClients []statsClientRow `json:"top_clients"`
}

// getStats serves the aggregate. The window parameter is a duration that must
// land on whole hours and may not exceed the configured retention; the
// default is the retention itself. A disabled retention answers zeros.
func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.StatsConfig(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	window := config.Interval
	if raw := r.URL.Query().Get("window"); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil {
			writeError(w, badRequest{fmt.Errorf("api: window %q: %w", raw, parseErr)})
			return
		}
		if parsed%time.Hour != 0 || parsed <= 0 {
			writeError(w, badRequest{errors.New("api: window must be a multiple of one hour")})
			return
		}
		if config.Interval != 0 && parsed > config.Interval {
			writeError(w, badRequest{fmt.Errorf("api: window %s exceeds the retained %s", parsed, config.Interval)})
			return
		}
		window = parsed
	}
	if window == 0 {
		writeJSON(w, http.StatusOK, emptyStats())
		return
	}

	summary, err := s.store.Stats(r.Context(), store.StatsRead{Since: time.Now().Add(-window).UnixMilli(), Limit: 10})
	if err != nil {
		writeError(w, err)
		return
	}

	series := make([]statsSeriesRow, 0, len(summary.Series))
	for _, bucket := range summary.Series {
		series = append(series, statsSeriesRow{Start: bucket.Start.UnixMilli(), Total: bucket.Total, Blocked: bucket.Blocked, Rewritten: bucket.Rewritten})
	}
	queried := make([]statsNameRow, 0, len(summary.TopQueried))
	for _, row := range summary.TopQueried {
		queried = append(queried, statsNameRow{Name: row.Name, Count: row.Count})
	}
	blocked := make([]statsNameRow, 0, len(summary.TopBlocked))
	for _, row := range summary.TopBlocked {
		blocked = append(blocked, statsNameRow{Name: row.Name, Count: row.Count})
	}
	clients := make([]statsClientRow, 0, len(summary.TopClients))
	for _, row := range summary.TopClients {
		clients = append(clients, statsClientRow{Client: row.Client, Count: row.Count})
	}
	writeJSON(w, http.StatusOK, statsResponse{
		Window:     window.Milliseconds(),
		Total:      summary.Total,
		Allowed:    summary.Allowed,
		Blocked:    summary.Blocked,
		Rewritten:  summary.Rewritten,
		Series:     series,
		TopQueried: queried,
		TopBlocked: blocked,
		TopClients: clients,
	})
}

func emptyStats() statsResponse {
	return statsResponse{
		Series:     []statsSeriesRow{},
		TopQueried: []statsNameRow{},
		TopBlocked: []statsNameRow{},
		TopClients: []statsClientRow{},
	}
}

// resetStats deletes the logged rows inside the retention window. The log
// page and the summary share one table; the design record owns that trade.
func (s *Server) resetStats(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.StatsConfig(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	window := config.Interval
	if window == 0 {
		writeError(w, badRequest{errors.New("api: stats are disabled, there is nothing to reset")})
		return
	}
	if err := s.store.ResetStats(r.Context(), time.Now().Add(-window).UnixMilli()); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
