package runtime

import (
	"fmt"

	"aegis/internal/filter"
	"aegis/internal/services"
	"aegis/internal/store"
)

// focusRules turns every focus window into the client-scoped scheduled block
// rules the engine indexes. A window names one schedule, one set of clients,
// and one set of catalog services; each pair becomes the same rule a
// hand-written client-scoped scheduled rule would be, so the scheduled layer
// needs no case for it.
func focusRules(cfg store.Config) ([]filter.RuleSpec, error) {
	if len(cfg.FocusWindows) == 0 {
		return nil, nil
	}
	byID := catalogByID(cfg)

	var rules []filter.RuleSpec
	for _, window := range cfg.FocusWindows {
		for _, client := range window.Clients {
			for _, id := range window.Services {
				service, exists := byID[id]
				if !exists {
					return nil, fmt.Errorf("runtime: focus window %q names service %q, which is not in the catalog", window.Name, id)
				}
				specs, _ := services.Specs(service, services.Scope{Client: client, Schedule: window.Schedule})
				rules = append(rules, specs...)
			}
		}
	}
	return rules, nil
}
