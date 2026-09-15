// Package runtime keeps what the DNS server answers from in step with the
// stored configuration.
package runtime

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"

	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/store"
)

// snapshot is one generation of everything a query needs. The rule set and the
// identity table are stored together and read with one load, so a query cannot
// pair the new rules with the old selectors while a reload is in flight.
type snapshot struct {
	set      *filter.RuleSet
	identity *client.Resolver
}

// Runtime owns the engine's contents and rebuilds them when the configuration
// changes. It is the only writer.
type Runtime struct {
	store *store.Store

	mu sync.Mutex
	// staticRules come from the blocklist files given at boot and never change
	// while running. sourceRules come from the enabled rows of the sources
	// table and are replaced by SourceSync on every refresh.
	staticRules []filter.RuleSpec
	sourceRules []filter.RuleSpec

	current atomic.Pointer[snapshot]
}

// New returns a Runtime that allows every query until Reload runs.
func New(s *store.Store, staticRules []filter.RuleSpec) *Runtime {
	return &Runtime{store: s, staticRules: staticRules}
}

// Reload reads the stored configuration, compiles it with the current rules,
// and publishes the result in one store. A query in flight finishes against the
// generation it loaded, so a reload never blocks the DNS path.
//
// A failure leaves the previous generation serving. A bad configuration must
// not take the resolver down.
func (r *Runtime) Reload(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publish(ctx)
}

// replaceSourceRules swaps the source rules and republishes under the same
// lock, so a query never pairs new source rules with an old snapshot.
func (r *Runtime) replaceSourceRules(ctx context.Context, rules []filter.RuleSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sourceRules = rules
	return r.publish(ctx)
}

func (r *Runtime) publish(ctx context.Context) error {
	cfg, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	identity, err := client.New(cfg.Selectors())
	if err != nil {
		return err
	}
	rules := make([]filter.RuleSpec, 0, len(r.staticRules)+len(r.sourceRules))
	rules = append(rules, r.staticRules...)
	rules = append(rules, r.sourceRules...)
	set, err := filter.Compile(filter.Config{
		Rules:    rules,
		Profiles: cfg.Profiles,
		Clients:  cfg.ClientSpecs(),
		Default:  cfg.Default,
	})
	if err != nil {
		return err
	}

	r.current.Store(&snapshot{set: set, identity: identity})
	return nil
}

// Decide answers one query for the client at address, from a single generation.
func (r *Runtime) Decide(name filter.Domain, address netip.Addr) filter.Verdict {
	current := r.current.Load()
	if current == nil {
		return filter.Verdict{Action: filter.ActionAllow}
	}
	return current.set.Decide(name, current.identity.Key(address))
}

// Size reports how many rules the current generation holds, so a start line can
// say what actually loaded rather than what was asked for.
func (r *Runtime) Size() int {
	current := r.current.Load()
	if current == nil {
		return 0
	}
	return current.set.Len()
}
