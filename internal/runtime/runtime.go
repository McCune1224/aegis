// Package runtime keeps what the DNS server answers from in step with the
// stored configuration.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"aegis/internal/blocklist"
	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/metrics"
	"aegis/internal/rewrite"
	"aegis/internal/store"
	"aegis/internal/upstream"
)

// ListFile is one blocklist file the operator named at boot. The runtime reads
// it on every publish, so an edit reaches a running server on the next reload
// without a restart.
type ListFile struct {
	Path   string
	Format blocklist.Format
}

// snapshot is one generation of everything a query needs. The rule set, the
// identity table, and the rewrite table are stored together and read with one
// load, so a query cannot pair the new rules with the old selectors while a
// reload is in flight.
type snapshot struct {
	set      *filter.RuleSet
	identity *client.Resolver
	rewrites *rewrite.Table
}

// Runtime owns the engine's contents and rebuilds them when the configuration
// changes. It is the only writer.
type Runtime struct {
	store    *store.Store
	logger   *slog.Logger
	now      func() time.Time
	counts   *metrics.Metrics
	switcher *upstream.Switch

	mu sync.Mutex
	// lists are the blocklist files given at boot. publish reads them on every
	// reload, so the file contents are inputs rather than frozen rules.
	lists []ListFile
	// sourceRules come from the enabled rows of the sources table and are
	// replaced by SourceSync on every refresh.
	sourceRules []filter.RuleSpec
	current     atomic.Pointer[snapshot]
}

// Option changes a Runtime at construction.
type Option func(*Runtime)

// WithClock names where Decide reads the wall clock, so a test can state the
// minute it asks about.
func WithClock(now func() time.Time) Option {
	return func(r *Runtime) { r.now = now }
}

// WithMetrics names where reload counts land. A nil Metrics counts nothing.
func WithMetrics(m *metrics.Metrics) Option {
	return func(r *Runtime) { r.counts = m }
}

// New returns a Runtime that allows every query until Reload runs.
func New(s *store.Store, lists []ListFile, logger *slog.Logger, opts ...Option) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Runtime{store: s, lists: lists, logger: logger, now: time.Now, switcher: upstream.NewSwitch()}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Upstreams is the resolver handle the cache and the DNS handler wire to once
// at boot. Reload swaps the pool it holds, so a stored change reaches every
// later query through the same handle.
func (r *Runtime) Upstreams() *upstream.Switch { return r.switcher }

// Reload reads the stored configuration, compiles it with the current rules,
// and publishes the result in one store. A query in flight finishes against the
// generation it loaded, so a reload never blocks the DNS path.
//
// A failure leaves the previous generation serving. A bad configuration must
// not take the resolver down.
func (r *Runtime) Reload(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.publish(ctx); err != nil {
		return err
	}
	if r.counts != nil {
		r.counts.CountReload()
	}
	return nil
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
	// Custom rules come first so they win the declaration-order tie-break
	// against a list rule of the same tier and specificity.
	listRules, err := r.readLists()
	if err != nil {
		return err
	}
	rules := make([]filter.RuleSpec, 0, len(cfg.Rules)+len(listRules)+len(r.sourceRules))
	rules = append(rules, cfg.Rules...)
	rules = append(rules, listRules...)
	rules = append(rules, r.sourceRules...)
	set, err := filter.Compile(filter.Config{
		Rules:     rules,
		Profiles:  cfg.Profiles,
		Clients:   cfg.ClientSpecs(),
		Schedules: cfg.Schedules,
		Default:   cfg.Default,
	})
	if err != nil {
		return err
	}

	pool, err := upstreamPool(cfg)
	if err != nil {
		return err
	}

	r.switcher.Swap(pool)
	r.current.Store(&snapshot{set: set, identity: identity, rewrites: rewrite.New(cfg.Rewrites)})
	return nil
}

// upstreamPool builds the resolver pool from the enabled rows. A disabled row
// drops out; a stored URL that no longer parses fails the reload with the row
// named, and a store without an enabled row fails the same way a pool built
// from nothing would.
func upstreamPool(cfg store.Config) (*upstream.Pool, error) {
	specs := make([]upstream.Spec, 0, len(cfg.Upstreams))
	for _, row := range cfg.Upstreams {
		if !row.Enabled {
			continue
		}
		spec, err := upstream.Parse(row.URL)
		if err != nil {
			return nil, fmt.Errorf("runtime: upstream %q: %w", row.Name, err)
		}
		specs = append(specs, spec)
	}
	return upstream.New(upstream.Config{Specs: specs})
}

// Decide answers one query for the client at address, from a single generation.
func (r *Runtime) Decide(name filter.Domain, address netip.Addr) filter.Verdict {
	current := r.current.Load()
	if current == nil {
		return filter.Verdict{Action: filter.ActionAllow}
	}
	return current.set.Decide(name, current.identity.Key(address), address, r.now())
}

// ClientKey names the identity policy keys on for one address, from the same
// generation Decide reads, so the rate limiter and the filter never disagree
// about who is asking.
func (r *Runtime) ClientKey(address netip.Addr) filter.ClientKey {
	current := r.current.Load()
	if current == nil {
		return ""
	}
	return current.identity.Key(address)
}

// Lookup maps one name through the rewrite table, from the same generation
// the filter reads, so a query cannot be rewritten by new rules and filtered
// by old ones. The handler calls it through the Rewriter seam.
func (r *Runtime) Lookup(name filter.Domain) (rewrite.Record, bool) {
	current := r.current.Load()
	if current == nil {
		return rewrite.Record{}, false
	}
	return current.rewrites.Lookup(name)
}

// Reverse maps one address back to the name that pins it, from the same
// generation as Lookup.
func (r *Runtime) Reverse(address netip.Addr) (filter.Domain, bool) {
	current := r.current.Load()
	if current == nil {
		return filter.Domain{}, false
	}
	return current.rewrites.Reverse(address)
}

// ValidateLists reads and parses every list file the way publish will, so a
// bad file fails the start before the database is created.
func ValidateLists(lists []ListFile) error {
	for _, list := range lists {
		if _, err := readListFile(list); err != nil {
			return err
		}
	}
	return nil
}

// readLists parses every blocklist file fresh. A file that cannot be read
// fails the publish, so the previous generation keeps serving rather than the
// resolver quietly dropping the rules the operator asked for.
func (r *Runtime) readLists() ([]filter.RuleSpec, error) {
	var rules []filter.RuleSpec
	for _, list := range r.lists {
		result, err := readListFile(list)
		if err != nil {
			return nil, err
		}
		r.logger.Info("blocklist loaded", "path", list.Path, "rules", len(result.Rules), "skipped", result.Skipped)
		rules = append(rules, result.Rules...)
	}
	return rules, nil
}

func readListFile(list ListFile) (blocklist.ParseResult, error) {
	file, err := os.Open(list.Path)
	if err != nil {
		return blocklist.ParseResult{}, fmt.Errorf("runtime: %w", err)
	}

	source := filter.Source{ID: list.Path, Name: filepath.Base(list.Path)}
	result, parseErr := blocklist.ParseList(file, source, list.Format)
	closeErr := file.Close()

	if parseErr != nil {
		return blocklist.ParseResult{}, parseErr
	}
	if closeErr != nil {
		return blocklist.ParseResult{}, fmt.Errorf("runtime: %w", closeErr)
	}
	return result, nil
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
