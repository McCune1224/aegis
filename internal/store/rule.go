package store

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"aegis/internal/filter"
	"aegis/internal/store/storedb"
)

// customSource is the provenance of every user-created rule, so a verdict can
// name the rule the dashboard offered the action from.
var customSource = filter.Source{ID: "custom", Name: "custom"}

// Rule is one user-created allow or block rule. The kind names the payload:
// a Domain for exact and subdomains, a Pattern for wildcard and regex, a
// Network for cidr.
type Rule struct {
	ID       int64
	Domain   filter.Domain
	Pattern  string
	Network  netip.Prefix
	Kind     filter.MatchKind
	Action   filter.Action
	Schedule string
	Client   filter.ClientKey
	Notes    string
	Created  time.Time
}

// Value is the text the rule matches on, which is what the store keeps in its
// single value column and what the API renders back.
func (r Rule) Value() string {
	switch r.Kind {
	case filter.MatchExact, filter.MatchSubdomains:
		return r.Domain.String()
	case filter.MatchWildcard, filter.MatchRegex:
		return r.Pattern
	case filter.MatchCIDR:
		return r.Network.String()
	}
	return ""
}

// Spec is the rule in the shape filter.Compile takes.
func (r Rule) Spec() filter.RuleSpec {
	return filter.RuleSpec{
		ID:       fmt.Sprintf("custom:%d", r.ID),
		Source:   customSource,
		Kind:     r.Kind,
		Domain:   r.Domain,
		Pattern:  r.Pattern,
		Network:  r.Network,
		Schedule: r.Schedule,
		Client:   r.Client,
		Action:   r.Action,
	}
}

// Rules returns every user-created rule.
func (s *Store) Rules(ctx context.Context) ([]Rule, error) {
	return s.loadRules(ctx)
}

func (s *Store) loadRules(ctx context.Context) ([]Rule, error) {
	rows, err := s.queries.ListRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: rules: %w", err)
	}
	rules := make([]Rule, 0, len(rows))
	for _, row := range rows {
		parsed, err := parseRule(row)
		if err != nil {
			return nil, err
		}
		rules = append(rules, parsed)
	}
	return rules, nil
}

func parseRule(row storedb.Rule) (Rule, error) {
	kind, err := filter.ParseMatchKind(row.Kind)
	if err != nil {
		return Rule{}, fmt.Errorf("store: rule %d: %w", row.ID, err)
	}
	rule := Rule{
		ID:       row.ID,
		Kind:     kind,
		Action:   filter.ActionBlock,
		Schedule: row.Schedule,
		Client:   filter.ClientKey(row.Client),
		Notes:    row.Notes,
		Created:  unixSeconds(row.Created),
	}
	switch kind {
	case filter.MatchExact, filter.MatchSubdomains:
		rule.Domain, err = filter.ParseDomain(row.Domain)
	case filter.MatchWildcard, filter.MatchRegex:
		rule.Pattern, err = filter.ParsePattern(kind, row.Domain)
	case filter.MatchCIDR:
		rule.Network, err = filter.ParseNetwork(row.Domain)
	}
	if err != nil {
		return Rule{}, fmt.Errorf("store: rule %d: %w", row.ID, err)
	}
	if rule.Action, err = filter.ParseAction(row.Action); err != nil {
		return Rule{}, fmt.Errorf("store: rule %d: %w", row.ID, err)
	}
	return rule, nil
}

// SaveRule inserts a rule and returns its ID, or replaces the row when the rule
// carries an ID.
func (s *Store) SaveRule(ctx context.Context, rule Rule) (int64, error) {
	params := storedb.InsertRuleParams{
		Domain:   rule.Value(),
		Kind:     rule.Kind.String(),
		Action:   rule.Action.String(),
		Notes:    rule.Notes,
		Schedule: rule.Schedule,
		Client:   string(rule.Client),
	}
	if !rule.Created.IsZero() {
		params.Created = rule.Created.Unix()
	}
	if rule.ID == 0 {
		id, err := s.queries.InsertRule(ctx, params)
		if err != nil {
			return 0, fmt.Errorf("store: save rule: %w", err)
		}
		return id, nil
	}
	if err := s.queries.UpdateRule(ctx, storedb.UpdateRuleParams{
		Domain:   params.Domain,
		Kind:     params.Kind,
		Action:   params.Action,
		Notes:    params.Notes,
		Created:  params.Created,
		Schedule: params.Schedule,
		Client:   params.Client,
		ID:       rule.ID,
	}); err != nil {
		return 0, fmt.Errorf("store: save rule %d: %w", rule.ID, err)
	}
	return rule.ID, nil
}

// DeleteRule removes one user-created rule.
func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	if err := s.queries.DeleteRule(ctx, id); err != nil {
		return fmt.Errorf("store: delete rule %d: %w", id, err)
	}
	return nil
}
