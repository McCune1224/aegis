// Package filter decides whether one DNS query is allowed or blocked, and names
// the rule that decided it.
package filter

import (
	"fmt"
	"strings"
)

// Domain is a parsed DNS name. It is always lowercase and holds no trailing
// dot, which is the only form the rule tables accept as a key.
type Domain struct {
	name string
}

// ParseDomain turns a name from DNS wire, config, or a blocklist file into a
// Domain. It is the boundary for names, so raw strings stop here and Domain
// values move inward. A name with a label DNS cannot carry is an error rather
// than a key that silently never matches.
func ParseDomain(raw string) (Domain, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return Domain{}, fmt.Errorf("filter: empty domain %q", raw)
	}
	if !validName(s) {
		return Domain{}, fmt.Errorf("filter: malformed domain %q", raw)
	}
	return Domain{name: s}, nil
}

// validName reports whether every label holds only bytes that can appear in a
// DNS name. It checks in place so a query does not allocate on the way in.
func validName(name string) bool {
	label := 0
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c == '.':
			if label == 0 {
				return false
			}
			label = 0
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			label++
		default:
			return false
		}
	}
	return label > 0
}

func (d Domain) String() string { return d.name }

// Action is what a matching rule does to a query.
type Action uint8

const (
	ActionBlock Action = iota
	ActionAllow

	actionCount
)

// actionTier orders two matching rules. A rule in a lower tier beats one in a
// higher tier before any other comparison, so an allow rule wins over a block
// rule however specific the block is and whatever order the lists loaded in.
// Adding a tier later, for rewrites or client overrides, is one more row.
var actionTier = [actionCount]int{
	ActionBlock: 1,
	ActionAllow: 0,
}

// MatchKind is how a rule's domain is compared against a query name.
type MatchKind uint8

const (
	MatchExact MatchKind = iota
	// MatchSubdomains covers the rule's domain and every name under it, so a
	// rule for example.com also covers ads.example.com.
	MatchSubdomains
)

// Source names the blocklist a rule came from.
type Source struct {
	ID   string
	Name string
}

// RuleSpec is one rule as it arrives from a parsed blocklist or from config.
type RuleSpec struct {
	ID     string
	Source Source
	Kind   MatchKind
	Domain Domain
	Action Action
}

// Provenance names the rule that produced a verdict. The query log and the
// dashboard read it, so a user can unblock by clicking the rule that fired.
type Provenance struct {
	RuleID  string
	Source  Source
	Pattern string
}

// Verdict is the decision for one query. A nil Match means no rule matched and
// the query is allowed by default. Policy belongs to the client, and it rides
// here so the caller answers without a second lookup.
type Verdict struct {
	Action Action
	Match  *Provenance
	Policy Policy
}

// RuleSet is an immutable rule index and policy table. Build it with Compile,
// then read it from any number of goroutines. Nothing mutates a RuleSet after
// Compile returns.
type RuleSet struct {
	exact      map[string]*entry
	subdomains map[string]*entry
	clients    map[ClientKey]Policy
	fallback   Policy
}

type entry struct {
	best *candidate
}

type candidate struct {
	provenance Provenance
	action     Action
	labels     int
	exact      bool
	order      int
}

// Compile resolves every profile and client and indexes the rules for lookup.
// It rejects a rule or a profile the tables cannot express instead of dropping
// it, so a bad config fails the load rather than failing open.
func Compile(cfg Config) (*RuleSet, error) {
	profiles, err := compileProfiles(cfg.Profiles, cfg.Default)
	if err != nil {
		return nil, err
	}
	clients, err := compileClients(cfg.Clients, profiles)
	if err != nil {
		return nil, err
	}

	rs := &RuleSet{
		exact:      make(map[string]*entry, len(cfg.Rules)),
		subdomains: make(map[string]*entry, len(cfg.Rules)),
		clients:    clients,
		fallback:   profiles[cfg.Default],
	}
	if err := rs.indexRules(cfg.Rules); err != nil {
		return nil, err
	}
	return rs, nil
}

func (rs *RuleSet) indexRules(specs []RuleSpec) error {
	for order, spec := range specs {
		if spec.Domain.name == "" {
			return fmt.Errorf("filter: rule %q has no domain", spec.ID)
		}
		if spec.Action != ActionAllow && spec.Action != ActionBlock {
			return fmt.Errorf("filter: rule %q has unknown action %d", spec.ID, spec.Action)
		}

		var (
			table map[string]*entry
			exact bool
		)
		switch spec.Kind {
		case MatchExact:
			table, exact = rs.exact, true
		case MatchSubdomains:
			table, exact = rs.subdomains, false
		default:
			return fmt.Errorf("filter: rule %q has unknown match kind %d", spec.ID, spec.Kind)
		}

		e := table[spec.Domain.name]
		if e == nil {
			e = &entry{}
			table[spec.Domain.name] = e
		}
		e.best = better(e.best, &candidate{
			provenance: Provenance{RuleID: spec.ID, Source: spec.Source, Pattern: spec.Domain.name},
			action:     spec.Action,
			labels:     countLabels(spec.Domain.name),
			exact:      exact,
			order:      order,
		})
	}
	return nil
}

// Decide returns the verdict for one name and one client. It looks up the
// client's policy once, then the whole name and each parent, so its cost
// follows the label count and not the rule count.
func (rs *RuleSet) Decide(name Domain, client ClientKey) Verdict {
	policy := rs.fallback
	if specific, exists := rs.clients[client]; exists {
		policy = specific
	}

	var best *candidate

	if e := rs.exact[name.name]; e != nil {
		best = better(best, e.best)
	}

	for start := 0; ; {
		label := name.name[start:]
		if e := rs.subdomains[label]; e != nil {
			best = better(best, e.best)
		}
		dot := strings.IndexByte(label, '.')
		if dot < 0 {
			break
		}
		start += dot + 1
	}

	if best == nil {
		return Verdict{Action: ActionAllow, Policy: policy}
	}
	return Verdict{Action: best.action, Match: &best.provenance, Policy: policy}
}

// better picks between two candidates for the same name. Tie-breaking is by
// tier, then specificity, then exact over subdomain, then declaration order, so
// the result never depends on map iteration.
func better(current, next *candidate) *candidate {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}
	if actionTier[next.action] != actionTier[current.action] {
		if actionTier[next.action] < actionTier[current.action] {
			return next
		}
		return current
	}
	if next.labels != current.labels {
		if next.labels > current.labels {
			return next
		}
		return current
	}
	if next.exact != current.exact {
		if next.exact {
			return next
		}
		return current
	}
	if next.order < current.order {
		return next
	}
	return current
}

func countLabels(name string) int {
	n := 1
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			n++
		}
	}
	return n
}
