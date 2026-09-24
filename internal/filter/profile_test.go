package filter_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
)

// profileScoped returns a block rule for one profile, which is how a service
// block or a profile-level policy reaches the engine. The scope is set on the
// spec, not on a schedule, because a service set is active every minute.
func profileScoped(id, name string, profile filter.ProfileID, action filter.Action) filter.RuleSpec {
	spec := hagezi(id, filter.MatchSubdomains, name)
	spec.Profile = profile
	spec.Action = action
	return spec
}

// twoProfiles is the smallest config that can tell two profiles apart: one
// client on each, plus the default profile nobody names.
func twoProfiles(specs []filter.RuleSpec) filter.Config {
	return filter.Config{
		Rules: specs,
		Profiles: []filter.ProfileSpec{
			{ID: "default"},
			{ID: "kids"},
			{ID: "adults"},
		},
		Clients: []filter.ClientSpec{
			{Key: "tablet", Profile: "kids"},
			{Key: "laptop", Profile: "adults"},
		},
		Default: "default",
	}
}

func compileProfiles(t *testing.T, cfg filter.Config) *filter.RuleSet {
	t.Helper()
	rs, err := filter.Compile(cfg)
	require.NoError(t, err)
	return rs
}

func TestProfileScopedRuleAnswersOnlyForClientsOnThatProfile(t *testing.T) {
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{
		profileScoped("block-video", "video.example", "kids", filter.ActionBlock),
	}))

	blocked := rs.Decide(domain(t, "video.example"), "tablet", noAddress, testNow)
	require.Equal(t, filter.ActionBlock, blocked.Action)
	require.NotNil(t, blocked.Match)
	require.Equal(t, "block-video", blocked.Match.RuleID)

	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "video.example"), "laptop", noAddress, testNow).Action)
}

func TestProfileScopedRuleIsActiveEveryMinute(t *testing.T) {
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{
		profileScoped("block-video", "video.example", "kids", filter.ActionBlock),
	}))

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "video.example"), "tablet", noAddress, wednesdayNoon).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "video.example"), "tablet", noAddress, wednesday21).Action)
}

func TestUnscopedRuleCoversEveryProfile(t *testing.T) {
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{
		hagezi("block-trackers", filter.MatchSubdomains, "tracker.example"),
	}))

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "tracker.example"), "tablet", noAddress, testNow).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "tracker.example"), "laptop", noAddress, testNow).Action)
}

func TestProfileScopedRuleReachesTheDefaultProfile(t *testing.T) {
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{
		profileScoped("block-video", "video.example", "default", filter.ActionBlock),
	}))

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "video.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "video.example"), "tablet", noAddress, testNow).Action)
}

func TestProfileScopeFiltersCandidacyWithoutChangingPrecedence(t *testing.T) {
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{
		hagezi("block-games", filter.MatchSubdomains, "games.example"),
		profileScoped("allow-games", "games.example", "kids", filter.ActionAllow),
	}))

	// On the scoped profile both rules are candidates, and the allow tier wins.
	allowed := rs.Decide(domain(t, "games.example"), "tablet", noAddress, testNow)
	require.Equal(t, filter.ActionAllow, allowed.Action)
	require.NotNil(t, allowed.Match)
	require.Equal(t, "allow-games", allowed.Match.RuleID)

	// Elsewhere only the unscoped block is a candidate.
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "games.example"), "laptop", noAddress, testNow).Action)
}

func TestProfileScopeBreaksTiesByDeclarationOrderAcrossScopes(t *testing.T) {
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{
		profileScoped("first-wins", "games.example", "kids", filter.ActionBlock),
		hagezi("listed", filter.MatchSubdomains, "games.example"),
	}))

	scoped := rs.Decide(domain(t, "games.example"), "tablet", noAddress, testNow)
	require.Equal(t, filter.ActionBlock, scoped.Action)
	require.Equal(t, "first-wins", scoped.Match.RuleID)

	unscoped := rs.Decide(domain(t, "games.example"), "laptop", noAddress, testNow)
	require.Equal(t, filter.ActionBlock, unscoped.Action)
	require.Equal(t, "listed", unscoped.Match.RuleID)
}

func TestClientScopedRuleAnswersOnlyForTheClientItNames(t *testing.T) {
	spec := hagezi("block-video", filter.MatchSubdomains, "video.example")
	spec.Client = "tablet"
	spec.Action = filter.ActionBlock
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{spec}))

	blocked := rs.Decide(domain(t, "video.example"), "tablet", noAddress, testNow)
	require.Equal(t, filter.ActionBlock, blocked.Action)
	require.NotNil(t, blocked.Match)
	require.Equal(t, "block-video", blocked.Match.RuleID)

	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "video.example"), "laptop", noAddress, testNow).Action)
}

func TestScheduledRuleScopeIsTheProfileItNames(t *testing.T) {
	cfg := twoProfiles([]filter.RuleSpec{
		scheduledRule("block-games", filter.MatchSubdomains, filter.ActionBlock, "games.example", "night", ""),
	})
	cfg.Rules[0].Profile = "kids"
	cfg.Schedules = []filter.ScheduleSpec{schedule("night", 1, allDays(21*60, 7*60))}
	rs := compileProfiles(t, cfg)

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "games.example"), "tablet", noAddress, wednesday21).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "games.example"), "tablet", noAddress, wednesdayNoon).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "games.example"), "laptop", noAddress, wednesday21).Action)
}

func TestProfileScopedPatternRuleMatchesOnlyItsProfile(t *testing.T) {
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{
		func() filter.RuleSpec {
			spec := ruleSpec("block-video", filter.MatchWildcard, filter.ActionBlock, "*.video.example")
			spec.Profile = "kids"
			return spec
		}(),
	}))

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "shop.video.example"), "tablet", noAddress, testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "shop.video.example"), "laptop", noAddress, testNow).Action)
}

func TestCompileRejectsARuleNamingBothAClientAndAProfile(t *testing.T) {
	cfg := twoProfiles([]filter.RuleSpec{{
		ID:      "both-scopes",
		Source:  filter.Source{ID: "hagezi", Name: "Hagezi"},
		Kind:    filter.MatchSubdomains,
		Domain:  mustParse("games.example"),
		Action:  filter.ActionBlock,
		Client:  "tablet",
		Profile: "kids",
	}})

	_, err := filter.Compile(cfg)
	require.ErrorContains(t, err, "both a client and a profile")
}

func TestCompileRejectsARuleNamingAnUndefinedProfile(t *testing.T) {
	cfg := twoProfiles([]filter.RuleSpec{
		profileScoped("block-video", "video.example", "ghosts", filter.ActionBlock),
	})

	_, err := filter.Compile(cfg)
	require.ErrorContains(t, err, "ghosts")
}

func TestProfileOfResolvesTheProfileAQueryIsScopedAgainst(t *testing.T) {
	rs := compileProfiles(t, twoProfiles(nil))

	require.Equal(t, filter.ProfileID("kids"), rs.ProfileOf("tablet"))
	require.Equal(t, filter.ProfileID("default"), rs.ProfileOf(""))
	require.Equal(t, filter.ProfileID("default"), rs.ProfileOf("nobody"))
}

func TestProfileScopeKeepsTheWallClockOutOfTheDecision(t *testing.T) {
	rs := compileProfiles(t, twoProfiles([]filter.RuleSpec{
		profileScoped("block-video", "video.example", "kids", filter.ActionBlock),
	}))

	// The same profile decides the same way at any minute, so a service block
	// cannot quietly depend on the schedule table.
	morning := time.Date(2026, 9, 16, 6, 30, 0, 0, time.UTC)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "video.example"), "tablet", noAddress, morning).Action)
}
