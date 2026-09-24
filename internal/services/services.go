// Package services owns the blocked-services catalog: the JSON AdGuard
// publishes, the DNS subset of the rule dialect it uses, and the
// profile-scoped filter rules an enabled service compiles to.
package services

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"aegis/internal/filter"
)

// DefaultCatalogURL is the catalog AdGuard Home builds its own service sets
// from, so a service set updates between Aegis releases instead of shipping in
// the binary.
const DefaultCatalogURL = "https://adguardteam.github.io/HostlistsRegistry/assets/services.json"

// Service is one catalog entry: the rules that carry it, the group it
// belongs to, and the inline icon the UI renders beside its name.
type Service struct {
	ID      string
	Name    string
	Group   string
	Rules   []string
	IconSVG string
}

// Catalog is the parsed services document.
type Catalog struct {
	Services []Service
	Groups   []string
}

type catalogDoc struct {
	Services []catalogService `json:"blocked_services"`
	Groups   []catalogGroup   `json:"groups"`
}

// catalogService accepts both spellings of the group field, because the
// published document has used each of them.
type catalogService struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Group   string   `json:"group"`
	GroupID string   `json:"group_id"`
	Rules   []string `json:"rules"`
	IconSVG string   `json:"icon_svg"`
}

type catalogGroup struct {
	ID string `json:"id"`
}

// ParseCatalog reads the services document. It is the boundary for the
// catalog, so a missing or duplicated id stops here rather than becoming a
// service an operator cannot enable.
func ParseCatalog(body []byte) (Catalog, error) {
	var doc catalogDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return Catalog{}, fmt.Errorf("services: catalog: %w", err)
	}

	catalog := Catalog{Groups: make([]string, 0, len(doc.Groups))}
	for _, group := range doc.Groups {
		if group.ID != "" {
			catalog.Groups = append(catalog.Groups, group.ID)
		}
	}

	seen := make(map[string]bool, len(doc.Services))
	catalog.Services = make([]Service, 0, len(doc.Services))
	for _, entry := range doc.Services {
		if entry.ID == "" {
			return Catalog{}, fmt.Errorf("services: a catalog entry has no id")
		}
		if seen[entry.ID] {
			return Catalog{}, fmt.Errorf("services: entry %q is defined twice", entry.ID)
		}
		seen[entry.ID] = true

		name := entry.Name
		if name == "" {
			name = entry.ID
		}
		group := entry.Group
		if group == "" {
			group = entry.GroupID
		}
		catalog.Services = append(catalog.Services, Service{
			ID:      entry.ID,
			Name:    name,
			Group:   group,
			Rules:   entry.Rules,
			IconSVG: entry.IconSVG,
		})
	}
	return catalog, nil
}

// Scope is where one enabled service's rules apply and when. A profile scope
// covers every client on that profile and is always on. A client scope covers
// one identity: always on by itself, and with a Schedule attached only while
// Schedule covers the query minute, which is the form a window uses. Action
// names the verdict the rules carry; a window that allows a service during its
// schedule emits allow rules through the same path.
type Scope struct {
	Profile  filter.ProfileID
	Client   filter.ClientKey
	Schedule string
	Action   filter.Action
}

// Specs converts the rules of one service into the rule specs that enabling it
// for a scope activates. The catalog's dialect is the AdGuard one; the forms
// without a DNS meaning are counted in skipped, the way a list import counts a
// line it cannot use, so one odd rule does not cost the service.
func Specs(service Service, scope Scope) ([]filter.RuleSpec, int) {
	source := filter.Source{ID: service.ID, Name: service.Name}
	specs := make([]filter.RuleSpec, 0, len(service.Rules))
	skipped := 0
	for index, line := range service.Rules {
		spec, ok := ruleSpec(line, source, scope, index+1)
		if !ok {
			skipped++
			continue
		}
		specs = append(specs, spec)
	}
	return specs, skipped
}

// ruleID names one generated rule. The service id and line are unique within a
// service, so only a client scope needs to add anything: two clients sharing a
// window must not share a provenance id.
func ruleID(service string, scope Scope, line int) string {
	id := service + ":" + strconv.Itoa(line)
	if scope.Client != "" {
		id += "~" + string(scope.Client)
	}
	return id
}

// ruleSpec reads one catalog rule. ||host^ covers the host and everything
// under it, the anchor AGH means by a service set; |host^ pins one host, which
// is how the catalog names a CDN edge; a pattern is taken as written.
func ruleSpec(text string, source filter.Source, scope Scope, line int) (filter.RuleSpec, bool) {
	rule := strings.TrimSpace(text)
	if rule == "" {
		return filter.RuleSpec{}, false
	}

	spec := filter.RuleSpec{
		ID:       ruleID(source.ID, scope, line),
		Source:   source,
		Action:   scope.Action,
		Profile:  scope.Profile,
		Client:   scope.Client,
		Schedule: scope.Schedule,
	}
	switch {
	case len(rule) > 1 && strings.HasPrefix(rule, "/") && strings.HasSuffix(rule, "/"):
		pattern, err := filter.ParsePattern(filter.MatchRegex, rule[1:len(rule)-1])
		if err != nil {
			return filter.RuleSpec{}, false
		}
		spec.Kind, spec.Pattern = filter.MatchRegex, pattern
		return spec, true
	case strings.HasPrefix(rule, "||"):
		return anchoredRule(spec, rule[2:], filter.MatchSubdomains)
	case strings.HasPrefix(rule, "|"):
		return anchoredRule(spec, rule[1:], filter.MatchExact)
	default:
		return wildcardRule(spec, rule)
	}
}

// anchoredRule cuts an anchored rule at its separator and turns what is left
// into a name rule, or a wildcard when it carries one.
func anchoredRule(spec filter.RuleSpec, rest string, kind filter.MatchKind) (filter.RuleSpec, bool) {
	name := rest
	if cut := strings.IndexAny(name, "^$/"); cut >= 0 {
		name = name[:cut]
	}
	if name == "" {
		return filter.RuleSpec{}, false
	}
	if strings.Contains(name, "*") {
		return wildcardRule(spec, name)
	}
	domain, err := filter.ParseDomain(name)
	if err != nil {
		return filter.RuleSpec{}, false
	}
	spec.Kind, spec.Domain = kind, domain
	return spec, true
}

// wildcardRule turns a wildcard rule into a pattern. Aegis wildcards stand for
// whole labels, so a catalog rule with a partial label, such as ebay-*,
// carries no DNS meaning here and is refused.
func wildcardRule(spec filter.RuleSpec, name string) (filter.RuleSpec, bool) {
	pattern, err := filter.ParsePattern(filter.MatchWildcard, name)
	if err != nil {
		return filter.RuleSpec{}, false
	}
	spec.Kind, spec.Pattern = filter.MatchWildcard, pattern
	return spec, true
}
