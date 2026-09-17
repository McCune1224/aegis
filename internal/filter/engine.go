package filter

import (
	"net/netip"
	"sync/atomic"
)

// Engine holds the rule set a running server answers from. Queries read the
// current set without a lock, and an update swaps the whole set in one store,
// so a query in flight sees one consistent set from start to finish.
type Engine struct {
	current atomic.Pointer[RuleSet]
}

// New returns an Engine that allows every query until Publish replaces the set.
func New() *Engine {
	e := &Engine{}
	e.current.Store(&RuleSet{})
	return e
}

// Snapshot returns the set the Engine answers from right now. Hold it to answer
// several queries against one consistent set.
func (e *Engine) Snapshot() *RuleSet {
	return e.current.Load()
}

// Publish makes next the set for every query from now on. It is safe to call
// while queries are running. A query already in flight finishes against the set
// it loaded, which the garbage collector keeps alive until that query returns.
func (e *Engine) Publish(next *RuleSet) {
	if next == nil {
		panic("filter: Publish(nil)")
	}
	e.current.Store(next)
}

// Decide answers one query for one client from one address against the set
// the Engine answers from now.
func (e *Engine) Decide(name Domain, client ClientKey, address netip.Addr) Verdict {
	return e.current.Load().Decide(name, client, address)
}
