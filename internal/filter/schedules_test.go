package filter_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
)

var (
	// testNow is the wall clock tests that do not care about schedules ask
	// at. Any fixed time works, because nothing they compile is time-scoped.
	testNow = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	// A Wednesday noon and a Wednesday evening in UTC, so a test states the
	// minute it asks about and no schedule covers it by accident.
	wednesdayNoon = testNow
	wednesday21   = time.Date(2026, 9, 16, 21, 5, 0, 0, time.UTC)
)

func schedule(name string, priority int, windows ...filter.Window) filter.ScheduleSpec {
	return filter.ScheduleSpec{Name: name, Priority: priority, Windows: windows}
}

func allDays(start, end int) filter.Window {
	return filter.Window{
		Days:  []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
		Start: start,
		End:   end,
	}
}

func scheduledRule(id string, kind filter.MatchKind, action filter.Action, value, scheduleName string, client filter.ClientKey) filter.RuleSpec {
	spec := ruleSpec(id, kind, action, value)
	spec.Schedule = scheduleName
	spec.Client = client
	return spec
}

func compileScheduled(t *testing.T, schedules []filter.ScheduleSpec, specs ...filter.RuleSpec) *filter.RuleSet {
	t.Helper()
	cfg := defaultConfig(specs)
	cfg.Schedules = schedules
	rs, err := filter.Compile(cfg)
	require.NoError(t, err)
	return rs
}

func TestScheduledRuleBlocksOnlyInsideItsWindow(t *testing.T) {
	rs := compileScheduled(t,
		[]filter.ScheduleSpec{schedule("night", 1, allDays(21*60, 7*60))},
		scheduledRule("block-games", filter.MatchSubdomains, filter.ActionBlock, "games.example", "night", ""),
	)

	blocked := rs.Decide(domain(t, "shop.games.example"), "", noAddress, wednesday21)
	require.Equal(t, filter.ActionBlock, blocked.Action)
	require.NotNil(t, blocked.Match)
	require.Equal(t, "block-games", blocked.Match.RuleID)

	for _, minute := range []struct {
		name string
		when time.Time
	}{{"evening start", time.Date(2026, 9, 16, 21, 0, 0, 0, time.UTC)}, {"late night", time.Date(2026, 9, 16, 23, 59, 0, 0, time.UTC)}, {"before seven", time.Date(2026, 9, 16, 6, 59, 0, 0, time.UTC)}} {
		got := rs.Decide(domain(t, "games.example"), "", noAddress, minute.when)
		require.Equal(t, filter.ActionBlock, got.Action, "minute=%s", minute.name)
	}

	outside := rs.Decide(domain(t, "games.example"), "", noAddress, wednesdayNoon)
	require.Equal(t, filter.ActionAllow, outside.Action)
	require.Nil(t, outside.Match)
}

func TestWindowCrossingMidnightCoversTheNextMorning(t *testing.T) {
	rs := compileScheduled(t,
		[]filter.ScheduleSpec{schedule("tuesday-night", 1, filter.Window{
			Days:  []time.Weekday{time.Tuesday},
			Start: 21 * 60,
			End:   7 * 60,
		})},
		scheduledRule("block-games", filter.MatchSubdomains, filter.ActionBlock, "games.example", "tuesday-night", ""),
	)

	tuesday22 := time.Date(2026, 9, 15, 22, 0, 0, 0, time.UTC)
	wednesday6 := time.Date(2026, 9, 16, 6, 30, 0, 0, time.UTC)
	wednesday22 := time.Date(2026, 9, 16, 22, 0, 0, 0, time.UTC)

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "games.example"), "", noAddress, tuesday22).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "games.example"), "", noAddress, wednesday6).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "games.example"), "", noAddress, wednesday22).Action)
}

func TestHigherPriorityWindowWinsAtOneMinute(t *testing.T) {
	rs := compileScheduled(t,
		[]filter.ScheduleSpec{
			schedule("quiet", 1, allDays(20*60, 22*60)),
			schedule("lockdown", 5, allDays(21*60, 23*60)),
		},
		scheduledRule("allow-games", filter.MatchExact, filter.ActionAllow, "games.example", "quiet", ""),
		scheduledRule("block-games", filter.MatchSubdomains, filter.ActionBlock, "games.example", "lockdown", ""),
	)

	// Both windows cover 21:05, so lockdown wins by priority.
	overlap := rs.Decide(domain(t, "games.example"), "", noAddress, wednesday21)
	require.Equal(t, filter.ActionBlock, overlap.Action)
	require.NotNil(t, overlap.Match)
	require.Equal(t, "block-games", overlap.Match.RuleID)

	// Only quiet covers 20:30, so its rule is the one active there.
	quietOnly := rs.Decide(domain(t, "games.example"), "", noAddress, time.Date(2026, 9, 16, 20, 30, 0, 0, time.UTC))
	require.Equal(t, filter.ActionAllow, quietOnly.Action)
	require.Equal(t, "allow-games", quietOnly.Match.RuleID)
}

func TestSamePriorityOverlapIsACompileError(t *testing.T) {
	cfg := defaultConfig(nil)
	cfg.Schedules = []filter.ScheduleSpec{
		schedule("quiet", 1, allDays(20*60, 22*60)),
		schedule("lockdown", 1, allDays(21*60, 23*60)),
	}
	_, err := filter.Compile(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "priority 1")
}

func TestDaylightSavingKeepsTheWallClockHour(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	rs := compileScheduled(t,
		[]filter.ScheduleSpec{schedule("night", 1, allDays(21*60, 7*60))},
		scheduledRule("block-games", filter.MatchSubdomains, filter.ActionBlock, "games.example", "night", ""),
	)

	// Spring forward: 02:30 EST does not exist and 03:00 EDT follows 01:59
	// EST, but the wall clock reads 03:00 either way.
	springForward := time.Date(2026, 3, 8, 3, 0, 0, 0, ny)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "games.example"), "", noAddress, springForward).Action)

	// Fall back: 01:30 happens twice on November 1, and both passes read the
	// same wall clock, so both get the same answer.
	fallBack := time.Date(2026, 11, 1, 1, 30, 0, 0, ny)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "games.example"), "", noAddress, fallBack).Action)

	sundayNoon := time.Date(2026, 11, 1, 12, 0, 0, 0, ny)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "games.example"), "", noAddress, sundayNoon).Action)
}

func TestScheduledRuleScopeIsTheClientItNames(t *testing.T) {
	rs := compileScheduled(t,
		[]filter.ScheduleSpec{schedule("night", 1, allDays(21*60, 7*60))},
		scheduledRule("block-games", filter.MatchSubdomains, filter.ActionBlock, "games.example", "night", "tablet"),
	)

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "games.example"), "tablet", noAddress, wednesday21).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "games.example"), "laptop", noAddress, wednesday21).Action)
}

func TestAllowInsideAWindowBeatsAListedBlock(t *testing.T) {
	rs := compileScheduled(t,
		[]filter.ScheduleSpec{schedule("homework", 1, allDays(15*60, 17*60))},
		hagezi("list-block", filter.MatchSubdomains, "games.example"),
		scheduledRule("allow-games", filter.MatchSubdomains, filter.ActionAllow, "games.example", "homework", ""),
	)

	inWindow := rs.Decide(domain(t, "games.example"), "", noAddress, time.Date(2026, 9, 16, 15, 30, 0, 0, time.UTC))
	require.Equal(t, filter.ActionAllow, inWindow.Action)
	require.Equal(t, "allow-games", inWindow.Match.RuleID)

	outOfWindow := rs.Decide(domain(t, "games.example"), "", noAddress, wednesdayNoon)
	require.Equal(t, filter.ActionBlock, outOfWindow.Action)
	require.Equal(t, "list-block", outOfWindow.Match.RuleID)
}

func TestScheduledPatternRulesMatch(t *testing.T) {
	rs := compileScheduled(t,
		[]filter.ScheduleSpec{schedule("night", 1, allDays(21*60, 7*60))},
		scheduledRule("block-games", filter.MatchWildcard, filter.ActionBlock, "*.games.example", "night", ""),
	)

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "shop.games.example"), "", noAddress, wednesday21).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "games.example"), "", noAddress, wednesday21).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "shop.games.example"), "", noAddress, wednesdayNoon).Action)
}

func TestCompileRejectsBrokenSchedules(t *testing.T) {
	cases := []struct {
		name      string
		schedules []filter.ScheduleSpec
		specs     []filter.RuleSpec
		want      string
	}{
		{
			name:      "a schedule with no windows",
			schedules: []filter.ScheduleSpec{schedule("empty", 1)},
			want:      "window",
		},
		{
			name: "a window with no days",
			schedules: []filter.ScheduleSpec{schedule("bad", 1, filter.Window{
				Start: 0, End: 60,
			})},
			want: "day",
		},
		{
			name: "a window with an unknown day",
			schedules: []filter.ScheduleSpec{schedule("bad", 1, filter.Window{
				Days: []time.Weekday{time.Sunday, time.Weekday(9)}, Start: 0, End: 60,
			})},
			want: "day",
		},
		{
			name: "a window with a minute out of range",
			schedules: []filter.ScheduleSpec{schedule("bad", 1, filter.Window{
				Days: []time.Weekday{time.Sunday}, Start: 0, End: 1440,
			})},
			want: "minute",
		},
		{
			name: "an empty window",
			schedules: []filter.ScheduleSpec{schedule("bad", 1, filter.Window{
				Days: []time.Weekday{time.Sunday}, Start: 60, End: 60,
			})},
			want: "minute",
		},
		{
			name:      "two schedules with the same name",
			schedules: []filter.ScheduleSpec{schedule("night", 1, allDays(0, 60)), schedule("night", 2, allDays(60, 120))},
			want:      "twice",
		},
		{
			name:      "a rule naming an unknown schedule",
			schedules: nil,
			specs:     []filter.RuleSpec{scheduledRule("r1", filter.MatchExact, filter.ActionBlock, "games.example", "night", "")},
			want:      "night",
		},
		{
			name:      "a rule naming a schedule for a name kind payload mismatch stays an error",
			schedules: []filter.ScheduleSpec{schedule("night", 1, allDays(0, 60))},
			specs: []filter.RuleSpec{{
				ID: "r1", Kind: filter.MatchExact, Schedule: "night", Action: filter.ActionBlock,
			}},
			want: "domain",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := defaultConfig(testCase.specs)
			cfg.Schedules = testCase.schedules
			_, err := filter.Compile(cfg)
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.want)
		})
	}
}

func TestParseMinuteOfDay(t *testing.T) {
	minute, err := filter.ParseMinuteOfDay("21:00")
	require.NoError(t, err)
	require.Equal(t, 21*60, minute)

	minute, err = filter.ParseMinuteOfDay("0:07")
	require.NoError(t, err)
	require.Equal(t, 7, minute)

	_, err = filter.ParseMinuteOfDay("24:00")
	require.Error(t, err)

	_, err = filter.ParseMinuteOfDay("noon")
	require.Error(t, err)
}
