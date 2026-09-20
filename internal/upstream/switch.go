package upstream

import (
	"context"
	"errors"
	"sync/atomic"

	mdns "github.com/miekg/dns"
)

// Switch is the stable handle a caller keeps while the runtime replaces the
// pool underneath it: the cache and the DNS handler wire to it once, and a
// reload swaps the pool every later query resolves through.
type Switch struct {
	pool atomic.Pointer[Pool]
}

// NewSwitch returns a switch with no pool. Resolve names the problem until
// the first pool is swapped in.
func NewSwitch() *Switch { return &Switch{} }

// Swap publishes the pool every later Resolve reads.
func (s *Switch) Swap(p *Pool) { s.pool.Store(p) }

// Resolve hands the query to the current pool.
func (s *Switch) Resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	pool := s.pool.Load()
	if pool == nil {
		return nil, errors.New("upstream: no resolvers are loaded")
	}
	return pool.Resolve(ctx, req)
}
