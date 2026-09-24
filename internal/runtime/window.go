package runtime

import (
	"fmt"

	"aegis/internal/filter"
	"aegis/internal/services"
	"aegis/internal/store"
)

// windowRules turns every window into the client-scoped scheduled rules the
// engine indexes. A window names one schedule, one action, one set of clients,
// and one set of catalog services; each pair becomes the same rule a
// hand-written scoped rule would be, with the window's verdict, so neither the
// scheduled layer nor the allow tier needs a case for it.
func windowRules(cfg store.Config) ([]filter.RuleSpec, error) {
	if len(cfg.ServiceWindows) == 0 {
		return nil, nil
	}
	byID := catalogByID(cfg)

	var rules []filter.RuleSpec
	for _, window := range cfg.ServiceWindows {
		for _, client := range window.Clients {
			for _, id := range window.Services {
				service, exists := byID[id]
				if !exists {
					return nil, fmt.Errorf("runtime: window %q names service %q, which is not in the catalog", window.Name, id)
				}
				specs, _ := services.Specs(service, services.Scope{
					Client:   client,
					Schedule: window.Schedule,
					Action:   window.Action,
				})
				rules = append(rules, specs...)
			}
		}
	}
	return rules, nil
}
