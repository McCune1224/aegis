// Package ratelimit refuses queries from clients that ask faster than their
// identity's allowance. One bucket covers one identity, so a device the client
// table claims has its own allowance and every unclaimed address shares the
// default one.
package ratelimit

import (
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"

	"golang.org/x/time/rate"

	"aegis/internal/filter"
)

// KeyFunc names the identity a source address carries. The runtime serves it
// from the same generation the filter reads, so a query and its rate bucket
// can never disagree about who is asking.
type KeyFunc func(netip.Addr) filter.ClientKey

// Config is what a Limiter needs.
type Config struct {
	Keys KeyFunc
	// Rate is queries per second, per identity. Burst is how many queries one
	// identity may spend in an instant before the rate takes over.
	Rate  float64
	Burst int
}

// Limiter holds one token bucket per identity. The table is keyed on the
// resolved identity, so it is bounded by the client table plus the default
// bucket and never grows with the number of source addresses that appear.
type Limiter struct {
	keys    KeyFunc
	rate    rate.Limit
	burst   int
	mu      sync.Mutex
	buckets map[filter.ClientKey]*rate.Limiter
	refused atomic.Int64
}

// New checks the config and returns a Limiter.
func New(cfg Config) (*Limiter, error) {
	if cfg.Keys == nil {
		return nil, errors.New("ratelimit: Config.Keys is required")
	}
	if cfg.Rate <= 0 {
		return nil, errors.New("ratelimit: Config.Rate must be positive")
	}
	if cfg.Burst < 1 {
		return nil, errors.New("ratelimit: Config.Burst must be at least one")
	}
	return &Limiter{
		keys:    cfg.Keys,
		rate:    rate.Limit(cfg.Rate),
		burst:   cfg.Burst,
		buckets: make(map[filter.ClientKey]*rate.Limiter),
	}, nil
}

// Allow spends one token for the identity at address and reports whether the
// query may proceed. A refused query is counted.
func (l *Limiter) Allow(address netip.Addr) bool {
	if !l.bucket(l.keys(address)).Allow() {
		l.refused.Add(1)
		return false
	}
	return true
}

// Refused counts the queries the limiter has turned away. Metrics do not exist
// yet (#56); this is the number they will surface, the same way the cache's
// hit rate waits on them.
func (l *Limiter) Refused() int64 {
	return l.refused.Load()
}

func (l *Limiter) bucket(key filter.ClientKey) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if bucket, ok := l.buckets[key]; ok {
		return bucket
	}
	bucket := rate.NewLimiter(l.rate, l.burst)
	l.buckets[key] = bucket
	return bucket
}
