package upstream

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	mdns "github.com/miekg/dns"
)

// Health constants. Two consecutive failures mark a resolver down for a
// backoff that doubles from ten seconds and caps at ten minutes; when the
// backoff elapses the resolver is eligible again and real queries probe it.
const (
	DefaultAttempt   = 2 * time.Second
	failureThreshold = 2
	backoffBase      = 10 * time.Second
	backoffMax       = 10 * time.Minute
	ewmaDivisor      = 4
)

// Config is what a Pool needs. Attempt is the budget one exchange gets before
// the pool gives up on that resolver for this query; zero takes the default.
type Config struct {
	Specs   []Spec
	Attempt time.Duration
	// Now names the wall clock, so a test can state when a backoff elapses.
	Now func() time.Time
	// TLS is the base TLS configuration the DoT and DoH transports clone.
	TLS *tls.Config
}

// peer is one configured resolver and its health.
type peer struct {
	spec      Spec
	transport transport
	ewma      time.Duration
	failures  int
	downUntil time.Time
}

// Pool answers allowed queries from the healthiest resolver it has, failing
// over to the next on error. One mutex covers the health table; the
// exchanges happen outside it.
type Pool struct {
	attempt time.Duration
	now     func() time.Time

	mu    sync.Mutex
	peers []*peer
}

// New checks the config and returns a Pool.
func New(cfg Config) (*Pool, error) {
	if len(cfg.Specs) == 0 {
		return nil, errors.New("upstream: at least one resolver is required")
	}
	attempt := cfg.Attempt
	if attempt <= 0 {
		attempt = DefaultAttempt
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	pool := &Pool{attempt: attempt, now: now}
	for _, spec := range cfg.Specs {
		pool.peers = append(pool.peers, &peer{spec: spec, transport: newTransport(spec, cfg.TLS)})
	}
	return pool, nil
}

// Resolve asks the healthiest resolver and walks the rest on failure. A
// SERVFAIL answer is an answer: it came from a resolver that is up, so it is
// returned rather than retried elsewhere. Every failure is one attempt under
// its own budget, so one dead resolver costs one timeout, not the caller's
// whole patience.
func (p *Pool) Resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	var lastErr error
	for _, candidate := range p.candidates() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := p.now()
		attempt, cancel := context.WithTimeout(ctx, p.attempt)
		resp, err := candidate.transport.Exchange(attempt, req)
		cancel()
		p.record(candidate, p.now().Sub(start), err)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("upstream: every resolver failed: %w", lastErr)
}

// candidates orders the resolvers one query may use. Healthy peers rank by
// measured latency, an unmeasured peer first so a new resolver is probed,
// ties broken by configured order. When every peer is down, the best-ranked
// one still gets tried, so a pool of dead resolvers degrades to one attempt
// per query instead of a new hard-failure mode.
func (p *Pool) candidates() []*peer {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	eligible := make([]*peer, 0, len(p.peers))
	for _, peer := range p.peers {
		if peer.failures < failureThreshold || !now.Before(peer.downUntil) {
			eligible = append(eligible, peer)
		}
	}
	if len(eligible) == 0 {
		return []*peer{slices.MinFunc(p.peers, func(a, b *peer) int { return rank(a) - rank(b) })}
	}
	slices.SortStableFunc(eligible, func(a, b *peer) int { return rank(a) - rank(b) })
	return eligible
}

// rank orders a peer for picking: an unmeasured peer first so it gets
// probed, then by measured latency.
func rank(p *peer) int {
	if p.ewma <= 0 {
		return -1
	}
	return int(p.ewma)
}

// record folds one exchange outcome into a peer's health. Latency averages
// only successful exchanges; failures count toward the down threshold and,
// once down, schedule the backoff that re-arms the probe.
func (p *Pool) record(peer *peer, sample time.Duration, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		peer.failures++
		if peer.failures >= failureThreshold {
			peer.downUntil = p.now().Add(backoffFor(peer.failures))
		}
		return
	}
	peer.failures = 0
	peer.downUntil = time.Time{}
	if sample <= 0 {
		return
	}
	if peer.ewma <= 0 {
		peer.ewma = sample
		return
	}
	peer.ewma += (sample - peer.ewma) / ewmaDivisor
}

// backoffFor doubles from the base for every failure past the threshold,
// capped, so a resolver that stays dead waits longer each time.
func backoffFor(failures int) time.Duration {
	backoff := backoffBase
	for i := failureThreshold; i < failures && backoff < backoffMax; i++ {
		backoff *= 2
	}
	return min(backoff, backoffMax)
}
