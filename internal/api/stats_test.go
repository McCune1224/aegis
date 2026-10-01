package api_test

import (
	"fmt"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
)

func statRow(t *testing.T, at time.Time, name string, verdict filter.Action) store.QueryEntry {
	t.Helper()
	return store.QueryEntry{
		Time:    at,
		Client:  netip.MustParseAddr("10.0.0.2"),
		Name:    mustDomain(t, name),
		Type:    "A",
		Verdict: verdict,
	}
}

func mustDomain(t *testing.T, name string) filter.Domain {
	t.Helper()
	domain, err := filter.ParseDomain(name)
	require.NoError(t, err)
	return domain
}

func seedStatsRows(t *testing.T, h *harness, entries []store.QueryEntry) {
	t.Helper()
	require.NoError(t, h.database.RecordQueries(t.Context(), entries))
}

func TestTheStatsEndpointAggregatesServerSide(t *testing.T) {
	h := startHarness(t)
	hourAgo := time.Now().Add(-time.Hour).Truncate(time.Hour)
	seedStatsRows(t, h, []store.QueryEntry{
		statRow(t, hourAgo, "ads.example.com", filter.ActionBlock),
		statRow(t, hourAgo.Add(time.Minute), "ads.example.com", filter.ActionBlock),
		statRow(t, hourAgo.Add(2*time.Minute), "example.com", filter.ActionAllow),
	})

	status, body := h.do(t, http.MethodGet, "/api/v1/stats?window=2h", "")
	require.Equal(t, http.StatusOK, status, body)
	require.JSONEq(t, fmt.Sprintf(`{
		"window": 7200000,
		"total": 3, "allowed": 1, "blocked": 2, "rewritten": 0,
		"series": [
			{"start": %d, "total": 3, "blocked": 2, "rewritten": 0}
		],
		"top_queried": [
			{"name": "ads.example.com", "count": 2},
			{"name": "example.com", "count": 1}
		],
		"top_blocked": [{"name": "ads.example.com", "count": 2}],
		"top_clients": [{"client": "10.0.0.2", "count": 3}]
	}`, hourAgo.UnixMilli()), body)
}

func TestTheStatsWindowIsCappedByTheConfiguredInterval(t *testing.T) {
	h := startHarness(t)

	require.Equal(t, http.StatusOK, mustDo(t, h, http.MethodPut, "/api/v1/stats/config", `{"interval":"24h"}`))

	status, body := h.do(t, http.MethodGet, "/api/v1/stats?window=720h", "")
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "window")

	status, body = h.do(t, http.MethodGet, "/api/v1/stats", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"window":86400000`, "the default window is the configured interval")
}

func TestTheStatsConfigRoundTripsAndIgnoredHostsChangeTheAnswer(t *testing.T) {
	h := startHarness(t)
	hourAgo := time.Now().Add(-time.Hour).Truncate(time.Hour)
	seedStatsRows(t, h, []store.QueryEntry{
		statRow(t, hourAgo, "noisy.example", filter.ActionBlock),
		statRow(t, hourAgo, "sub.noisy.example", filter.ActionBlock),
		statRow(t, hourAgo, "quiet.example", filter.ActionAllow),
	})

	status, body := h.do(t, http.MethodGet, "/api/v1/stats/config", "")
	require.Equal(t, http.StatusOK, status, body)
	require.JSONEq(t, `{"interval":"24h","ignored":[]}`, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/stats/config", `{"interval":"7d","ignored":["Noisy.Example"]}`)
	require.Equal(t, http.StatusOK, status, body)
	require.JSONEq(t, `{"interval":"168h","ignored":["noisy.example"]}`, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/stats", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"total":1`, "the ignored host and its subdomain stay out")
	require.Contains(t, body, "quiet.example")
	require.NotContains(t, body, "noisy.example")

	status, body = h.do(t, http.MethodPut, "/api/v1/stats/config", `{"interval":"90m"}`)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "interval")
}

func TestStatsResetEmptiesTheWindow(t *testing.T) {
	h := startHarness(t)
	hourAgo := time.Now().Add(-time.Hour)
	seedStatsRows(t, h, []store.QueryEntry{
		statRow(t, hourAgo, "a.example", filter.ActionAllow),
	})

	status, _ := h.do(t, http.MethodPost, "/api/v1/stats/reset", "")
	require.Equal(t, http.StatusNoContent, status)

	status, body := h.do(t, http.MethodGet, "/api/v1/stats", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"total":0`)

	rows, err := h.database.Queries(t.Context(), store.QueryFilter{})
	require.NoError(t, err)
	require.Empty(t, rows, "the reset removes the rows, the log page loses the span too")
}

func mustDo(t *testing.T, h *harness, method, path, body string) int {
	t.Helper()
	status, _ := h.do(t, method, path, body)
	return status
}
