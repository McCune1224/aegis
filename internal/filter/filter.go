// Package filter decides whether one DNS query is allowed or blocked, and names
// the rule that decided it.
package filter

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"time"
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

	// ActionRewrite is never a rule verdict. Compile rejects a rule that
	// carries it; the handler uses it to log a query it answered locally
	// from a rewrite.
	ActionRewrite

	actionCount
)

var actionNames = [actionCount]string{
	ActionBlock:   "block",
	ActionAllow:   "allow",
	ActionRewrite: "rewrite",
}

func (a Action) String() string {
	if int(a) >= len(actionNames) {
		return fmt.Sprintf("action(%d)", uint8(a))
	}
	return actionNames[a]
}

// ParseAction turns a stored or requested name into an Action. The names table
// is the single source, so a new action is one row and both directions keep
// working.
func ParseAction(name string) (Action, error) {
	for action, candidate := range actionNames {
		if candidate == name {
			return Action(action), nil
		}
	}
	return 0, fmt.Errorf("filter: unknown action %q", name)
}

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
	// MatchWildcard matches names built from the pattern's labels, where a *
	// label stands for exactly one arbitrary label, the way a DNS zone
	// wildcard does. Unlike subdomains, *.ads.example covers srv.ads.example
	// but not ads.example itself and not a.b.ads.example.
	MatchWildcard
	// MatchRegex matches the name against a Go regular expression. Matching is
	// case-insensitive and unanchored, so ^ and $ belong to the author.
	MatchRegex
	// MatchCIDR matches the address the query came from against a network.
	MatchCIDR

	matchKindCount
)

var matchKindNames = [matchKindCount]string{
	MatchExact:      "exact",
	MatchSubdomains: "subdomains",
	MatchWildcard:   "wildcard",
	MatchRegex:      "regex",
	MatchCIDR:       "cidr",
}

func (k MatchKind) String() string {
	if int(k) >= len(matchKindNames) {
		return fmt.Sprintf("kind(%d)", uint8(k))
	}
	return matchKindNames[k]
}

// ParseMatchKind turns a stored or requested name into a MatchKind, from the
// same names table String answers from.
func ParseMatchKind(name string) (MatchKind, error) {
	for kind, candidate := range matchKindNames {
		if candidate == name {
			return MatchKind(kind), nil
		}
	}
	return 0, fmt.Errorf("filter: unknown match kind %q", name)
}

// Source names the blocklist a rule came from.
type Source struct {
	ID   string
	Name string
}

// RuleSpec is one rule as it arrives from a parsed blocklist or from config.
// The kind names which payload it matches on: a Domain for exact and
// subdomains, a Pattern for wildcard and regex, a Network for cidr. Compile
// rejects a rule whose payload does not fit its kind. A rule may name a
// Schedule, which keeps it active only while that schedule covers the query
// minute, and a Client, which keeps it scoped to one identity; a scope
// without a schedule has no meaning, so Compile rejects it.
type RuleSpec struct {
	ID       string
	Source   Source
	Kind     MatchKind
	Domain   Domain
	Pattern  string
	Network  netip.Prefix
	Schedule string
	Client   ClientKey
	Action   Action
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
	// Route names the upstream a routed query must use. The runtime fills it
	// after Decide, from the same snapshot generation, and the filter itself
	// never sets it; empty means the pool may choose.
	Route string
}

// RuleSet is an immutable rule index and policy table. Build it with Compile,
// then read it from any number of goroutines. Nothing mutates a RuleSet after
// Compile returns.
type RuleSet struct {
	exact      map[string]*entry
	subdomains map[string]*entry
	patterns   []patternRule
	networks   []networkRule
	schedules  []compiledSchedule
	minutes    [minutesPerWeek]uint16
	clients    map[ClientKey]Policy
	fallback   Policy
	rules      int
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

// patternRule is one compiled wildcard or regex rule. A wildcard compiles to
// an anchored regexp, so both kinds match through one path and Decide never
// branches on which of the two it is looking at.
type patternRule struct {
	candidate candidate
	re        *regexp.Regexp
}

// networkRule is one CIDR rule. The slice is sorted longest prefix first, so
// the first prefix that contains the address is the most specific one.
type networkRule struct {
	prefix netip.Prefix
	best   *candidate
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
	schedules, minutes, err := compileSchedules(cfg.Schedules)
	if err != nil {
		return nil, err
	}

	rs := &RuleSet{
		exact:      make(map[string]*entry, len(cfg.Rules)),
		subdomains: make(map[string]*entry, len(cfg.Rules)),
		schedules:  schedules,
		minutes:    minutes,
		clients:    clients,
		fallback:   profiles[cfg.Default],
	}
	byName := make(map[string]int, len(schedules))
	for i, schedule := range schedules {
		byName[schedule.name] = i
	}
	if err := rs.indexRules(cfg.Rules, byName); err != nil {
		return nil, err
	}
	return rs, nil
}

// Len is how many rules the set was built from, which is more than the number
// of index keys when several rules claim one name.
func (rs *RuleSet) Len() int { return rs.rules }

func (rs *RuleSet) indexRules(specs []RuleSpec, scheduleIndex map[string]int) error {
	rs.rules = len(specs)
	byNetwork := make(map[netip.Prefix]*entry)
	for order, spec := range specs {
		if spec.Action != ActionAllow && spec.Action != ActionBlock {
			return fmt.Errorf("filter: rule %q has unknown action %d", spec.ID, spec.Action)
		}
		if spec.Client != "" && spec.Schedule == "" {
			return fmt.Errorf("filter: rule %q names a client but no schedule", spec.ID)
		}

		switch spec.Kind {
		case MatchExact, MatchSubdomains:
			if spec.Domain.name == "" {
				return fmt.Errorf("filter: rule %q needs a domain", spec.ID)
			}
			if spec.Pattern != "" || spec.Network.IsValid() {
				return fmt.Errorf("filter: rule %q carries a payload kind %s does not use", spec.ID, spec.Kind)
			}
			candidate := candidate{
				provenance: Provenance{RuleID: spec.ID, Source: spec.Source, Pattern: spec.Domain.name},
				action:     spec.Action,
				labels:     countLabels(spec.Domain.name),
				exact:      spec.Kind == MatchExact,
				order:      order,
			}
			if spec.Schedule != "" {
				if err := rs.schedule(spec, scheduleIndex, scheduledRule{
					candidate: candidate,
					client:    spec.Client,
					match:     nameMatcher(spec.Kind, spec.Domain, nil),
				}); err != nil {
					return err
				}
				continue
			}
			table := rs.exact
			if spec.Kind == MatchSubdomains {
				table = rs.subdomains
			}
			e := table[spec.Domain.name]
			if e == nil {
				e = &entry{}
				table[spec.Domain.name] = e
			}
			e.best = better(e.best, &candidate)
		case MatchWildcard, MatchRegex:
			if spec.Domain.name != "" || spec.Network.IsValid() {
				return fmt.Errorf("filter: rule %q carries a payload kind %s does not use", spec.ID, spec.Kind)
			}
			re, err := compilePattern(spec.Kind, spec.Pattern)
			if err != nil {
				return fmt.Errorf("filter: rule %q: %w", spec.ID, err)
			}
			candidate := candidate{
				provenance: Provenance{RuleID: spec.ID, Source: spec.Source, Pattern: spec.Pattern},
				action:     spec.Action,
				order:      order,
			}
			if spec.Schedule != "" {
				if err := rs.schedule(spec, scheduleIndex, scheduledRule{
					candidate: candidate,
					client:    spec.Client,
					match:     nameMatcher(spec.Kind, Domain{}, re),
				}); err != nil {
					return err
				}
				continue
			}
			rs.patterns = append(rs.patterns, patternRule{candidate: candidate, re: re})
		case MatchCIDR:
			if spec.Schedule != "" || spec.Client != "" {
				return fmt.Errorf("filter: rule %q cannot schedule a cidr rule", spec.ID)
			}
			if spec.Domain.name != "" || spec.Pattern != "" || !spec.Network.IsValid() {
				return fmt.Errorf("filter: rule %q needs a network and no other payload", spec.ID)
			}
			e := byNetwork[spec.Network]
			if e == nil {
				e = &entry{}
				byNetwork[spec.Network] = e
			}
			e.best = better(e.best, &candidate{
				provenance: Provenance{RuleID: spec.ID, Source: spec.Source, Pattern: spec.Network.String()},
				action:     spec.Action,
				order:      order,
			})
		default:
			return fmt.Errorf("filter: rule %q has unknown match kind %d", spec.ID, spec.Kind)
		}
	}

	rs.networks = make([]networkRule, 0, len(byNetwork))
	for prefix, e := range byNetwork {
		rs.networks = append(rs.networks, networkRule{prefix: prefix, best: e.best})
	}
	sort.Slice(rs.networks, func(i, j int) bool {
		return rs.networks[i].prefix.Bits() > rs.networks[j].prefix.Bits()
	})
	return nil
}

// schedule places one time-scoped rule into the schedule it names, refusing a
// rule that names a schedule the config does not define.
func (rs *RuleSet) schedule(spec RuleSpec, scheduleIndex map[string]int, rule scheduledRule) error {
	index, exists := scheduleIndex[spec.Schedule]
	if !exists {
		return fmt.Errorf("filter: rule %q names schedule %q, which is not defined", spec.ID, spec.Schedule)
	}
	rs.schedules[index].rules = append(rs.schedules[index].rules, rule)
	return nil
}

// nameMatcher bakes the comparison one name rule performs into a function, so
// the scheduled layer never branches on the kind while answering. A nil re is
// only passed by the exact and subdomains kinds.
func nameMatcher(kind MatchKind, domain Domain, re *regexp.Regexp) func(string) bool {
	switch kind {
	case MatchExact:
		target := domain.name
		return func(name string) bool { return name == target }
	case MatchSubdomains:
		target := "." + domain.name
		return func(name string) bool { return name == target[1:] || strings.HasSuffix(name, target) }
	default:
		return func(name string) bool { return re.MatchString(name) }
	}
}

// Decide returns the verdict for one name, one client, the address the query
// came from, and the wall clock the query arrived at, in the zone the caller
// means. It looks up the client's policy once, then the whole name and each
// parent, so its cost follows the label count and not the rule count.
//
// A rule that matches the client's network outranks any rule that only matches
// the domain: an exempt network is exempt however blocking the lists are, and
// a locked network is locked however permissive the domain rules are. Within
// one dimension the tiers, specificity, and declaration order decide, so the
// result never depends on map iteration. Time-scoped rules join the same
// comparison when their schedule covers the minute.
func (rs *RuleSet) Decide(name Domain, client ClientKey, address netip.Addr, now time.Time) Verdict {
	policy := rs.fallback
	if specific, exists := rs.clients[client]; exists {
		policy = specific
	}

	if best := rs.networkMatch(address); best != nil {
		return Verdict{Action: best.action, Match: &best.provenance, Policy: policy}
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

	// A pattern ranks below an indexed rule of the same tier, since a pattern
	// carries no specificity to compare. The action is the tier winner either
	// way, so this orders only the provenance.
	for i := range rs.patterns {
		if rs.patterns[i].re.MatchString(name.name) {
			best = better(best, &rs.patterns[i].candidate)
		}
	}

	// The minute table names the schedule that owns this minute of the week;
	// its rules join the comparison like any other candidate.
	if index := rs.minutes[minuteOfWeek(now)]; index != 0 {
		layer := &rs.schedules[index-1]
		for i := range layer.rules {
			rule := &layer.rules[i]
			if rule.client != "" && rule.client != client {
				continue
			}
			if rule.match(name.name) {
				best = better(best, &rule.candidate)
			}
		}
	}

	if best == nil {
		return Verdict{Action: ActionAllow, Policy: policy}
	}
	return Verdict{Action: best.action, Match: &best.provenance, Policy: policy}
}

// networkMatch returns the most specific CIDR rule that claims the address.
func (rs *RuleSet) networkMatch(address netip.Addr) *candidate {
	if len(rs.networks) == 0 || !address.IsValid() {
		return nil
	}
	addr := address.Unmap()
	for i := range rs.networks {
		if rs.networks[i].prefix.Contains(addr) {
			return rs.networks[i].best
		}
	}
	return nil
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
