package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
	"aegis/internal/services"
	"aegis/internal/store"
)

// ServiceSync fetches the blocked-services catalog and keeps the stored copy in
// step with it. The boot refresh and the background refresh both come here, and
// the API's manual refresh is the same call, so a catalog fetched between
// releases reaches an operator without a restart.
type ServiceSync struct {
	store   *store.Store
	fetcher *blocklist.Fetcher
	engine  *Runtime
	url     string
	logger  *slog.Logger

	mu sync.Mutex
}

// NewServiceSync returns a ServiceSync that fetches url and publishes the
// catalog it holds into engine.
func NewServiceSync(database *store.Store, fetcher *blocklist.Fetcher, url string, engine *Runtime, logger *slog.Logger) *ServiceSync {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServiceSync{store: database, fetcher: fetcher, engine: engine, url: url, logger: logger}
}

// RefreshCatalog downloads the catalog, replaces the stored copy, and
// republishes the runtime. A fetch or parse failure leaves the stored catalog
// serving, so a catalog that cannot be reached does not empty what an operator
// already enabled.
func (s *ServiceSync) RefreshCatalog(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fetched, err := s.fetcher.Fetch(ctx, s.url, "")
	if err != nil {
		return err
	}
	catalog, err := services.ParseCatalog(fetched.Body)
	if err != nil {
		return err
	}
	if err := s.store.SaveCatalog(ctx, time.Now().UTC(), catalog.Services); err != nil {
		return err
	}
	s.logger.Info("services catalog loaded", "services", len(catalog.Services), "url", s.url)
	return s.engine.Reload(ctx)
}

// serviceRules turns every profile's enabled services into the block rules the
// filter engine indexes, scoped to the profile that enabled them. The enable
// list is stored in profile then service order, so declaration-order ties do
// not shuffle between reloads.
func serviceRules(cfg store.Config) ([]filter.RuleSpec, error) {
	if len(cfg.ProfileServices) == 0 {
		return nil, nil
	}

	byID := catalogByID(cfg)

	var rules []filter.RuleSpec
	for _, enable := range cfg.ProfileServices {
		service, exists := byID[enable.Service]
		if !exists {
			return nil, fmt.Errorf("runtime: profile %q blocks service %q, which is not in the catalog", enable.Profile, enable.Service)
		}
		specs, _ := services.Specs(service, services.Scope{Profile: enable.Profile})
		rules = append(rules, specs...)
	}
	return rules, nil
}

// catalogByID indexes the fetched catalog by service id, the form both the
// profile enablements and the focus windows name a service by.
func catalogByID(cfg store.Config) map[string]services.Service {
	byID := make(map[string]services.Service, len(cfg.Services))
	for _, row := range cfg.Services {
		byID[row.ID] = services.Service{ID: row.ID, Name: row.Name, Group: row.Group, Rules: row.Rules}
	}
	return byID
}
