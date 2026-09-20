package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
)

func TestSaveRuleRoundTripsThroughLoad(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	created := time.Unix(1757900000, 0).UTC()

	id, err := s.SaveRule(ctx, store.Rule{
		Domain:  mustDomain(t, "games.example.com"),
		Kind:    filter.MatchExact,
		Action:  filter.ActionAllow,
		Notes:   "the spare laptop",
		Created: created,
	})
	require.NoError(t, err)
	require.Positive(t, id)

	rule, err := s.Rules(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Rule{{
		ID:      id,
		Domain:  mustDomain(t, "games.example.com"),
		Kind:    filter.MatchExact,
		Action:  filter.ActionAllow,
		Notes:   "the spare laptop",
		Created: created,
	}}, rule)

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, []filter.RuleSpec{{
		ID:     fmt.Sprintf("custom:%d", id),
		Source: filter.Source{ID: "custom", Name: "custom"},
		Kind:   filter.MatchExact,
		Domain: mustDomain(t, "games.example.com"),
		Action: filter.ActionAllow,
	}}, cfg.Rules)
}

func TestSaveRuleWithAnIDUpdatesTheRow(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	id, err := s.SaveRule(ctx, store.Rule{
		Domain: mustDomain(t, "games.example.com"),
		Kind:   filter.MatchSubdomains,
		Action: filter.ActionAllow,
	})
	require.NoError(t, err)

	_, err = s.SaveRule(ctx, store.Rule{
		ID:      id,
		Domain:  mustDomain(t, "games.example.com"),
		Kind:    filter.MatchSubdomains,
		Action:  filter.ActionBlock,
		Notes:   "changed our minds",
		Created: time.Unix(1757900001, 0).UTC(),
	})
	require.NoError(t, err)

	rules, err := s.Rules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, id, rules[0].ID)
	require.Equal(t, filter.ActionBlock, rules[0].Action)
	require.Equal(t, "changed our minds", rules[0].Notes)
}

func TestDeleteRuleRemovesIt(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	id, err := s.SaveRule(ctx, store.Rule{
		Domain: mustDomain(t, "games.example.com"),
		Kind:   filter.MatchExact,
		Action: filter.ActionAllow,
	})
	require.NoError(t, err)
	deleted, err := s.DeleteRule(ctx, id)
	require.NoError(t, err)
	require.True(t, deleted, "the rule the test just wrote is there to delete")

	rules, err := s.Rules(ctx)
	require.NoError(t, err)
	require.Empty(t, rules)
}

func TestLoadReportsAStoredRuleItCannotParse(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	// MatchKind(99) serializes to a name no parser accepts, which is what a
	// hand-edited row looks like to Load.
	_, err := s.SaveRule(ctx, store.Rule{
		Domain: mustDomain(t, "games.example.com"),
		Kind:   filter.MatchKind(99),
		Action: filter.ActionAllow,
	})
	require.NoError(t, err)

	_, err = s.Load(ctx)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown match kind")
}

func TestValidateCompilesTheStoredRules(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	_, err := s.SaveRule(ctx, store.Rule{
		Domain: mustDomain(t, "games.example.com"),
		Kind:   filter.MatchExact,
		Action: filter.ActionAllow,
	})
	require.NoError(t, err)

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}
