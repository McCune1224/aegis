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
// values move inward.
func ParseDomain(raw string) (Domain, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return Domain{}, fmt.Errorf("filter: empty domain %q", raw)
	}
	if strings.HasPrefix(s, ".") || strings.Contains(s, "..") {
		return Domain{}, fmt.Errorf("filter: malformed domain %q", raw)
	}
	return Domain{name: s}, nil
}

func (d Domain) String() string { return d.name }

// Action is what a matching rule does to a query.
type Action uint8

const (
	ActionBlock Action = iota
	ActionAllow
)

// MatchKind is how a rule's domain is compared against a query name.
type MatchKind uint8

const (
	MatchExact MatchKind = iota
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
// the query is allowed by default.
type Verdict struct {
	Action Action
	Match  *Provenance
}

// RuleSet is an immutable rule index. Build it with Compile, then read it from
// any number of goroutines. Nothing mutates a RuleSet after Compile returns.
type RuleSet struct {
	exact      map[string]*entry
	subdomains map[string]*entry
}

type entry struct {
	allow *candidate
	block *candidate
}

type candidate struct {
	provenance Provenance
	labels     int
	exact      bool
	order      int
}

// Compile indexes rules for lookup. It rejects a rule the tables cannot express
// instead of dropping it, so a bad list fails the load rather than failing open.
func Compile(specs []RuleSpec) (*RuleSet, error) {
	rs := &RuleSet{
		exact:      make(map[string]*entry, len(specs)),
		subdomains: make(map[string]*entry, len(specs)),
	}
	for order, spec := range specs {
		if spec.Domain.name == "" {
			return nil, fmt.Errorf("filter: rule %q has no domain", spec.ID)
		}
		if spec.Action != ActionAllow && spec.Action != ActionBlock {
			return nil, fmt.Errorf("filter: rule %q has unknown action %d", spec.ID, spec.Action)
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
			return nil, fmt.Errorf("filter: rule %q has unknown match kind %d", spec.ID, spec.Kind)
		}

		e := table[spec.Domain.name]
		if e == nil {
			e = &entry{}
			table[spec.Domain.name] = e
		}
		e.add(spec.Action, &candidate{
			provenance: Provenance{RuleID: spec.ID, Source: spec.Source, Pattern: spec.Domain.name},
			labels:     countLabels(spec.Domain.name),
			exact:      exact,
			order:      order,
		})
	}
	return rs, nil
}

func (e *entry) add(action Action, c *candidate) {
	if action == ActionAllow {
		e.allow = better(e.allow, c)
		return
	}
	e.block = better(e.block, c)
}

// Decide returns the verdict for one name. It looks up the whole name and then
// each parent, so its cost follows the label count and not the rule count.
func (rs *RuleSet) Decide(name Domain) Verdict {
	var allow, block *candidate

	if e := rs.exact[name.name]; e != nil {
		allow = better(allow, e.allow)
		block = better(block, e.block)
	}

	for start := 0; ; {
		label := name.name[start:]
		if e := rs.subdomains[label]; e != nil {
			allow = better(allow, e.allow)
			block = better(block, e.block)
		}
		dot := strings.IndexByte(label, '.')
		if dot < 0 {
			break
		}
		start += dot + 1
	}

	if allow != nil {
		return Verdict{Action: ActionAllow, Match: &allow.provenance}
	}
	if block != nil {
		return Verdict{Action: ActionBlock, Match: &block.provenance}
	}
	return Verdict{Action: ActionAllow}
}

// better picks between two candidates for the same action. Tie-breaking is by
// specificity, then exact over subdomain, then declaration order, so the result
// never depends on map iteration.
func better(current, next *candidate) *candidate {
	if current == nil {
		return next
	}
	if next == nil {
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
