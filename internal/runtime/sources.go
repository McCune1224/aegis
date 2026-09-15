package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"time"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
	"aegis/internal/store"
)

// SourceSync fetches the enabled blocklist sources and hands their rules to the
// Runtime. Serve calls it once at boot; the API calls it after every source
// write, which is what makes a change live without a restart.
type SourceSync struct {
	store   *store.Store
	fetcher *blocklist.Fetcher
	engine  *Runtime
	logger  *slog.Logger
}

// NewSourceSync returns a SourceSync that fetches through fetcher and publishes
// into engine.
func NewSourceSync(database *store.Store, fetcher *blocklist.Fetcher, engine *Runtime, logger *slog.Logger) *SourceSync {
	if logger == nil {
		logger = slog.Default()
	}
	return &SourceSync{store: database, fetcher: fetcher, engine: engine, logger: logger}
}

// Refresh downloads every enabled source and republishes the runtime with the
// rules they hold. A source that fails still contributes its cached body and
// its error is recorded on the source, so one broken list cannot take the
// resolver down or quietly empty it. An error return means the store or the
// republish failed, not that a fetch did.
func (s *SourceSync) RefreshSources(ctx context.Context) error {
	sources, err := s.store.EnabledSources(ctx)
	if err != nil {
		return err
	}

	rules := make([]filter.RuleSpec, 0)
	for _, source := range sources {
		fetched, fetchErr := s.fetcher.Fetch(ctx, source.URL, source.ETag)
		body := source.Body
		etag := source.ETag
		if fetchErr == nil && !fetched.NotModified {
			body = fetched.Body
			etag = fetched.ETag
		}

		ruleCount := 0
		if len(body) > 0 {
			result, parseErr := blocklist.ParseList(bytes.NewReader(body), filter.Source{ID: source.Name, Name: source.Name}, source.Format)
			switch {
			case parseErr != nil && fetchErr == nil:
				fetchErr = parseErr
			case parseErr == nil:
				rules = append(rules, result.Rules...)
				ruleCount = len(result.Rules)
				s.logger.Info("blocklist source loaded", "source", source.Name, "rules", ruleCount, "skipped", result.Skipped)
			}
		}

		if fetchErr != nil {
			s.logger.Warn("blocklist source failed", "source", source.Name, "url", source.URL, "error", fetchErr)
		}
		if err := s.store.RecordSourceFetch(ctx, source.Name, etag, time.Now(), fetchErr, ruleCount, body); err != nil {
			return err
		}
	}

	return s.engine.replaceSourceRules(ctx, rules)
}
