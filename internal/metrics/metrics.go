// Package metrics owns every counter the /metrics endpoint exposes. Hot paths
// bump plain atomics, and the Prometheus registry reads them at scrape time,
// so no query pays for the endpoint and a slow scrape cannot slow one down.
package metrics

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"aegis/internal/filter"
)

// CacheCounters is the snapshot the cache feeds in. Metrics declares the shape
// instead of importing the cache, so the dependency points one way.
type CacheCounters struct {
	Hits       uint64
	Misses     uint64
	Evictions  uint64
	Prefetches uint64
}

// UpstreamStat is the snapshot the upstream pool feeds in, one per configured
// resolver. Metrics declares the shape instead of importing the pool, so the
// dependency points one way.
type UpstreamStat struct {
	Name     string
	EWMA     time.Duration
	Failures int
	Down     bool
}

// Metrics is the counter set. Watchers must be wired before the HTTP server
// starts serving, which the serve command does while it builds the wiring.
type Metrics struct {
	registry *prometheus.Registry

	allowed   atomic.Uint64
	blocked   atomic.Uint64
	rewritten atomic.Uint64
	reloads   atomic.Uint64

	drops func() uint64
	cache func() CacheCounters

	upstreams func() []UpstreamStat
}

// New builds the counter set and its registry. The registry is private, so
// aegis never publishes another package's default collectors by accident.
func New() *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry()}

	verdict := func(name string, read func() uint64) {
		m.registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name:        "aegis_queries_total",
			Help:        "DNS queries by verdict.",
			ConstLabels: prometheus.Labels{"verdict": name},
		}, func() float64 { return float64(read()) }))
	}
	verdict("allowed", m.allowed.Load)
	verdict("blocked", m.blocked.Load)
	verdict("rewritten", m.rewritten.Load)

	m.registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "aegis_reloads_total",
		Help: "Configuration reloads that compiled and published.",
	}, func() float64 { return float64(m.reloads.Load()) }))

	m.registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "aegis_stream_drops_total",
		Help: "Query-stream events dropped because a subscriber fell behind.",
	}, func() float64 { return float64(m.readDrops()) }))

	cache := func(name, help string, read func(CacheCounters) uint64) {
		m.registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name: "aegis_cache_" + name + "_total",
			Help: help,
		}, func() float64 {
			return float64(read(m.readCache()))
		}))
	}
	cache("hits", "Answers served from the response cache.", func(c CacheCounters) uint64 { return c.Hits })
	cache("misses", "Queries the response cache asked the upstream for.", func(c CacheCounters) uint64 { return c.Misses })
	cache("evictions", "Cache entries evicted at the size bound.", func(c CacheCounters) uint64 { return c.Evictions })
	cache("prefetches", "Background cache refreshes started.", func(c CacheCounters) uint64 { return c.Prefetches })

	latencyDesc := prometheus.NewDesc("aegis_upstream_latency_seconds",
		"Smoothed query latency per upstream resolver.", []string{"upstream"}, nil)
	failuresDesc := prometheus.NewDesc("aegis_upstream_failures",
		"Consecutive failed exchanges per upstream resolver.", []string{"upstream"}, nil)
	downDesc := prometheus.NewDesc("aegis_upstream_down",
		"Whether the resolver is in its failure backoff, 1 or 0.", []string{"upstream"}, nil)
	m.registry.MustRegister(prometheus.CollectorFunc(func(ch chan<- prometheus.Metric) {
		for _, stat := range m.readUpstreams() {
			ch <- prometheus.MustNewConstMetric(latencyDesc, prometheus.GaugeValue, stat.EWMA.Seconds(), stat.Name)
			ch <- prometheus.MustNewConstMetric(failuresDesc, prometheus.GaugeValue, float64(stat.Failures), stat.Name)
			down := 0.0
			if stat.Down {
				down = 1
			}
			ch <- prometheus.MustNewConstMetric(downDesc, prometheus.GaugeValue, down, stat.Name)
		}
	}))

	return m
}

// CountVerdict records one decided query. The handler's observer calls it on
// the DNS worker, so it must stay lock-free.
func (m *Metrics) CountVerdict(action filter.Action) {
	switch action {
	case filter.ActionAllow:
		m.allowed.Add(1)
	case filter.ActionBlock:
		m.blocked.Add(1)
	case filter.ActionRewrite:
		m.rewritten.Add(1)
	}
}

// CountReload records one configuration reload that compiled and published.
func (m *Metrics) CountReload() { m.reloads.Add(1) }

// WatchDrops names where the stream-drop count lives. The hub owns the atomic;
// metrics reads it rather than keeping a second count.
func (m *Metrics) WatchDrops(f func() uint64) { m.drops = f }

// WatchCache names where the cache counters live.
func (m *Metrics) WatchCache(f func() CacheCounters) { m.cache = f }

// WatchUpstreams names where the upstream health snapshot lives.
func (m *Metrics) WatchUpstreams(f func() []UpstreamStat) { m.upstreams = f }

// Handler serves the exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) readDrops() uint64 {
	if m.drops == nil {
		return 0
	}
	return m.drops()
}

func (m *Metrics) readCache() CacheCounters {
	if m.cache == nil {
		return CacheCounters{}
	}
	return m.cache()
}

func (m *Metrics) readUpstreams() []UpstreamStat {
	if m.upstreams == nil {
		return nil
	}
	return m.upstreams()
}
