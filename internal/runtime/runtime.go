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

	"aegis/internal/blocklist"
	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/store"
)

// ListFile is one blocklist file the operator named at boot. The runtime reads
// it on every publish, so an edit reaches a running server on the next reload
// without a restart.
type ListFile struct {
	Path   string
	Format blocklist.Format
}

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
	store  *store.Store
	logger *slog.Logger

	mu sync.Mutex
	// lists are the blocklist files given at boot. publish reads them on every
	// reload, so the file contents are inputs rather than frozen rules.
	lists []ListFile
	// sourceRules come from the enabled rows of the sources table and are
	// replaced by SourceSync on every refresh.
	sourceRules []filter.RuleSpec
	current     atomic.Pointer[snapshot]
}

// New returns a Runtime that allows every query until Reload runs.
func New(s *store.Store, lists []ListFile, logger *slog.Logger) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runtime{store: s, lists: lists, logger: logger}
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
