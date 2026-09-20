package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/metrics"
)

func scrape(t *testing.T, m *metrics.Metrics) string {
	t.Helper()
	server := httptest.NewServer(m.Handler())
	t.Cleanup(server.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return readAll(t, resp.Body)
}

func valueFor(t *testing.T, body, line string) string {
	t.Helper()
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, line) {
			return strings.TrimSpace(strings.TrimPrefix(l, line))
		}
	}
	t.Fatalf("metrics text has no line for %s:\n%s", line, body)
	return ""
}

func TestScrapeReportsQueriesByVerdict(t *testing.T) {
	m := metrics.New()
	m.CountVerdict(filter.ActionAllow)
	m.CountVerdict(filter.ActionBlock)
	m.CountVerdict(filter.ActionBlock)

	body := scrape(t, m)
	require.Equal(t, "1", valueFor(t, body, `aegis_queries_total{verdict="allowed"}`))
	require.Equal(t, "2", valueFor(t, body, `aegis_queries_total{verdict="blocked"}`))
	require.Equal(t, "0", valueFor(t, body, `aegis_queries_total{verdict="rewritten"}`))
}

func TestScrapeReportsReloadsDropsAndCache(t *testing.T) {
	m := metrics.New()
	m.CountReload()
	m.CountReload()
	m.WatchDrops(func() uint64 { return 7 })
	m.WatchCache(func() metrics.CacheCounters {
		return metrics.CacheCounters{Hits: 3, Misses: 1, Evictions: 0, Prefetches: 1}
	})

	body := scrape(t, m)
	require.Equal(t, "2", valueFor(t, body, "aegis_reloads_total"))
	require.Equal(t, "7", valueFor(t, body, "aegis_stream_drops_total"))
	require.Equal(t, "3", valueFor(t, body, "aegis_cache_hits_total"))
	require.Equal(t, "1", valueFor(t, body, "aegis_cache_misses_total"))
	require.Equal(t, "0", valueFor(t, body, "aegis_cache_evictions_total"))
	require.Equal(t, "1", valueFor(t, body, "aegis_cache_prefetches_total"))
}

func TestScrapeReportsUpstreamHealth(t *testing.T) {
	m := metrics.New()
	m.WatchUpstreams(func() []metrics.UpstreamStat {
		return []metrics.UpstreamStat{
			{Name: "primary", EWMA: 1500 * time.Microsecond, Failures: 2, Down: true},
			{Name: "secondary"},
		}
	})

	body := scrape(t, m)
	require.Equal(t, "0.0015", valueFor(t, body, `aegis_upstream_latency_seconds{upstream="primary"}`))
	require.Equal(t, "2", valueFor(t, body, `aegis_upstream_failures{upstream="primary"}`))
	require.Equal(t, "1", valueFor(t, body, `aegis_upstream_down{upstream="primary"}`))
	require.Equal(t, "0", valueFor(t, body, `aegis_upstream_latency_seconds{upstream="secondary"}`),
		"an unmeasured resolver reads zero latency")
	require.Equal(t, "0", valueFor(t, body, `aegis_upstream_failures{upstream="secondary"}`))
	require.Equal(t, "0", valueFor(t, body, `aegis_upstream_down{upstream="secondary"}`))
}

func TestScrapeWithoutWatchersReportsZero(t *testing.T) {
	m := metrics.New()
	body := scrape(t, m)
	require.Equal(t, "0", valueFor(t, body, "aegis_stream_drops_total"), "no drops watcher, no drops")
	require.Equal(t, "0", valueFor(t, body, "aegis_cache_hits_total"), "no cache watcher, no hits")
	require.NotContains(t, body, "aegis_upstream_", "no upstream watcher, no resolver series")
}

func readAll(t *testing.T, body io.Reader) string {
	t.Helper()
	payload, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(payload)
}
