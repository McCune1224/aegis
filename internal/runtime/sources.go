package runtime

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
	"aegis/internal/store"
)

// SourceSync fetches the enabled blocklist sources and hands their rules to the
// Runtime. Serve calls RefreshSources once at boot; the API calls it after
// every source write, RefreshOne for a manual refresh, and RefreshDue from the
// background schedule. A mutex serializes all of them, so two refreshes cannot
// interleave their fetches and publishes.
type SourceSync struct {
	store   *store.Store
	fetcher *blocklist.Fetcher
	engine  *Runtime
	logger  *slog.Logger

	mu    sync.Mutex
	rules map[string][]filter.RuleSpec
}

// NewSourceSync returns a SourceSync that fetches through fetcher and publishes
// into engine.
func NewSourceSync(database *store.Store, fetcher *blocklist.Fetcher, engine *Runtime, logger *slog.Logger) *SourceSync {
	if logger == nil {
		logger = slog.Default()
	}
	return &SourceSync{
		store:   database,
		fetcher: fetcher,
		engine:  engine,
		logger:  logger,
		rules:   make(map[string][]filter.RuleSpec),
	}
}

// RefreshSources downloads every enabled source and republishes the runtime
// with the rules they hold. A source that fails still contributes its cached
// body and its error is recorded on the source, so one broken list cannot take
// the resolver down or quietly empty it. An error return means the store or the
// republish failed, not that a fetch did.
func (s *SourceSync) RefreshSources(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshAll(ctx)
}

// RefreshOne refreshes a single source by name and republishes. A disabled or
// unknown source is an error.
func (s *SourceSync) RefreshOne(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sources, err := s.store.EnabledSources(ctx)
	if err != nil {
		return err
	}
	var source *store.Source
	for index, candidate := range sources {
		if candidate.Name == name {
			source = &sources[index]
			break
		}
	}
	if source == nil {
		return fmt.Errorf("runtime: source %q is not enabled", name)
	}

	if err := s.refreshOne(ctx, *source); err != nil {
		return err
	}
	return s.publish(ctx, sources)
}

// RefreshDue refreshes every enabled source whose own schedule has elapsed,
// falling back to defaultInterval for sources without an override. It reports
// how many sources were fetched.
func (s *SourceSync) RefreshDue(ctx context.Context, now time.Time, defaultInterval time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sources, err := s.store.EnabledSources(ctx)
	if err != nil {
		return 0, err
	}

	fetched := 0
	for _, source := range sources {
		interval := defaultInterval
		if source.RefreshSeconds > 0 {
			interval = time.Duration(source.RefreshSeconds) * time.Second
		}
		if source.Fresh(now, interval) {
			continue
		}
		if err := s.refreshOne(ctx, source); err != nil {
			return fetched, err
		}
		fetched++
	}
	if fetched > 0 {
		if err := s.publish(ctx, sources); err != nil {
			return fetched, err
		}
	}
	return fetched, nil
}

// PreviewSource fetches the source's remote body and diffs it against the
// stored one. It records nothing and republishes nothing.
func (s *SourceSync) PreviewSource(ctx context.Context, name string) (added, removed []string, notModified bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sources, err := s.store.Sources(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	var source *store.Source
	for index, candidate := range sources {
		if candidate.Name == name {
			source = &sources[index]
			break
		}
	}
	if source == nil {
		return nil, nil, false, fmt.Errorf("runtime: source %q is not configured", name)
	}

	fetched, err := s.fetcher.Fetch(ctx, source.URL, source.ETag)
	if err != nil {
		return nil, nil, false, err
	}
	if fetched.NotModified {
		return nil, nil, true, nil
	}

	diff, err := blocklist.DiffLists(bytes.NewReader(source.Body), bytes.NewReader(fetched.Body),
		filter.Source{ID: source.Name, Name: source.Name}, source.Format)
	if err != nil {
		return nil, nil, false, err
	}
	return diff.Added, diff.Removed, false, nil
}

// refreshOne fetches, parses, and records one source, updating the per-source
// rule mirror. The caller holds the mutex and publishes. On any failure the
// last good body, counts, ETag, and last-fetch time stay untouched, so the
// source reads as stale rather than empty, and the failures counter climbs.
func (s *SourceSync) refreshOne(ctx context.Context, source store.Source) error {
	fetched, fetchErr := s.fetcher.Fetch(ctx, source.URL, source.ETag)

	body, etag := source.Body, source.ETag
	ruleCount, skipped := source.RuleCount, source.Skipped
	var ok bool

	if fetchErr == nil && fetched.NotModified {
		ok = true
	}
	if fetchErr == nil && !fetched.NotModified {
		result, parseErr := blocklist.ParseList(bytes.NewReader(fetched.Body), filter.Source{ID: source.Name, Name: source.Name}, source.Format)
		if parseErr != nil {
			fetchErr = parseErr
		} else {
			ok = true
			body = fetched.Body
			etag = fetched.ETag
			ruleCount = len(result.Rules)
			skipped = result.Skipped
			s.rules[source.Name] = result.Rules
			s.logger.Info("blocklist source loaded", "source", source.Name, "rules", ruleCount, "skipped", skipped)
		}
	}

	fetchedAt := time.Now()
	if !ok {
		if fetchErr != nil {
			s.logger.Warn("blocklist source failed", "source", source.Name, "url", source.URL, "error", fetchErr)
		}
		if len(source.Body) > 0 {
			// The cached list keeps serving through the failure.
			if result, parseErr := blocklist.ParseList(bytes.NewReader(source.Body), filter.Source{ID: source.Name, Name: source.Name}, source.Format); parseErr == nil {
				s.rules[source.Name] = result.Rules
			}
		}
		body, etag = source.Body, source.ETag
		ruleCount, skipped = source.RuleCount, source.Skipped
		fetchedAt = source.LastFetch
	}
	if err := s.store.RecordSourceFetch(ctx, source.Name, etag, fetchedAt, fetchErr, ruleCount, skipped, body); err != nil {
		return err
	}
	if _, mirrored := s.rules[source.Name]; !mirrored {
		// A 304 or a failure keeps the stored body, so the mirror is rebuilt
		// from it instead of the fetch, and a restarted sync serves the list
		// the store already holds.
		if result, parseErr := blocklist.ParseList(bytes.NewReader(body), filter.Source{ID: source.Name, Name: source.Name}, source.Format); parseErr == nil {
			s.rules[source.Name] = result.Rules
		}
	}
	return nil
}

// refreshAll refreshes every enabled source, prunes rule sets of sources that
// have gone, and republishes once.
func (s *SourceSync) refreshAll(ctx context.Context) error {
	sources, err := s.store.EnabledSources(ctx)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := s.refreshOne(ctx, source); err != nil {
			return err
		}
	}
	return s.publish(ctx, sources)
}

// publish hands the rule mirror to the runtime, keeping only enabled sources
// and in a stable name order so declaration-order tie-breaks do not shuffle.
func (s *SourceSync) publish(ctx context.Context, sources []store.Source) error {
	enabled := make(map[string]bool, len(sources))
	for _, source := range sources {
		enabled[source.Name] = true
	}
	for name := range s.rules {
		if !enabled[name] {
			delete(s.rules, name)
		}
	}

	names := make([]string, 0, len(s.rules))
	for name := range s.rules {
		names = append(names, name)
	}
	sort.Strings(names)

	rules := make([]filter.RuleSpec, 0)
	for _, name := range names {
		rules = append(rules, s.rules[name]...)
	}
	return s.engine.replaceSourceRules(ctx, rules)
}
