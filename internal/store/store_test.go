package store_test

import (
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func modePtr(mode filter.BlockingMode) *filter.BlockingMode { return &mode }

func TestOpenMigratesAndSeedsTheDefaultProfile(t *testing.T) {
	s := open(t)

	cfg, err := s.Load(t.Context())

	require.NoError(t, err)
	require.Equal(t, filter.ProfileID("default"), cfg.Default)
	require.Len(t, cfg.Profiles, 1)
	require.Equal(t, filter.ProfileID("default"), cfg.Profiles[0].ID)
	require.NotNil(t, cfg.Profiles[0].Mode)
	require.Equal(t, filter.NXDomain, *cfg.Profiles[0].Mode)
	require.Empty(t, cfg.Clients)
}

func TestOpeningAnExistingDatabaseKeepsWhatIsInIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aegis.db")

	first, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	require.NoError(t, first.SaveProfile(t.Context(), filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.Refused)}))
	require.NoError(t, first.Close())

	second, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	cfg, err := second.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, cfg.Profiles, 2)
}

func TestFirstBootIsRecordedOnce(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	first, err := s.FirstBoot(ctx)
	require.NoError(t, err)
	require.True(t, first)

	require.NoError(t, s.MarkSeeded(ctx))

	first, err = s.FirstBoot(ctx)
	require.NoError(t, err)
	require.False(t, first, "a seeded store must not look empty just because it has one profile")
}

func TestLoadRoundTripsAProfileAndAClientsSelectors(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	tablet := netip.MustParseAddr("10.9.9.2")
	subnet := netip.MustParsePrefix("10.9.8.0/24")

	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.Refused)}))
	require.NoError(t, s.SaveClient(ctx, store.Client{
		Key:       "tablet",
		Profile:   "kids",
		Notes:     "the spare one",
		Addresses: []netip.Addr{tablet},
		Prefixes:  []netip.Prefix{subnet},
	}))
	require.NoError(t, s.SetDefaultProfile(ctx, "kids"))

	cfg, err := s.Load(ctx)
	require.NoError(t, err)

	require.Equal(t, filter.ProfileID("kids"), cfg.Default)
	require.Equal(t, []client.Spec{{Key: "tablet", Addresses: []netip.Addr{tablet}, Prefixes: []netip.Prefix{subnet}}}, cfg.Selectors())
	require.Equal(t, []filter.ClientSpec{{Key: "tablet", Profile: "kids"}}, cfg.ClientSpecs())
	require.Equal(t, []store.Client{{Key: "tablet", Profile: "kids", Notes: "the spare one", Addresses: []netip.Addr{tablet}, Prefixes: []netip.Prefix{subnet}}}, cfg.Clients)

	// The stored values have to be usable, not merely present.
	set, err := filter.Compile(filter.Config{
		Profiles: cfg.Profiles,
		Clients:  cfg.ClientSpecs(),
		Default:  cfg.Default,
	})
	require.NoError(t, err)
	require.Equal(t, filter.Refused, set.Decide(mustDomain(t, "example.com"), "tablet", netip.Addr{}, time.Now()).Policy.Mode)

	resolver, err := client.New(cfg.Selectors())
	require.NoError(t, err)
	require.Equal(t, filter.ClientKey("tablet"), resolver.Key(tablet))
	require.Equal(t, filter.ClientKey("tablet"), resolver.Key(netip.MustParseAddr("10.9.8.9")))
}

func TestSavingAClientReplacesItsSelectors(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	first := netip.MustParseAddr("10.9.9.2")
	second := netip.MustParseAddr("10.9.9.3")

	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "tablet", Profile: "default", Addresses: []netip.Addr{first}}))
	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "tablet", Profile: "default", Addresses: []netip.Addr{second}}))

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Clients, 1)
	require.Equal(t, []netip.Addr{second}, cfg.Clients[0].Addresses)
}

func TestLoadReportsAStoredValueItCannotParse(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	nonsense := filter.BlockingMode(99)
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "broken", Mode: &nonsense}))

	_, err := s.Load(ctx)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown blocking mode")
}

func TestDeleteClientAlsoRemovesItsSelectors(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	tablet := netip.MustParseAddr("10.9.9.2")
	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "tablet", Profile: "default", Addresses: []netip.Addr{tablet}}))
	require.NoError(t, s.DeleteClient(ctx, "tablet"))

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Empty(t, cfg.Clients)
}

func TestDeleteProfileRemovesIt(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.Refused)}))
	require.NoError(t, s.DeleteProfile(ctx, "kids"))

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Profiles, 1)
}

func TestValidateRejectsAProfileCycle(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	cfg.Profiles = append(cfg.Profiles,
		filter.ProfileSpec{ID: "a", Extends: "b"},
		filter.ProfileSpec{ID: "b", Extends: "a"},
	)

	err = cfg.Validate()

	require.Error(t, err)
	require.Contains(t, err.Error(), "extends itself")
}

func TestSchedulesRoundTripAndReachTheEngine(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	night := filter.ScheduleSpec{
		Name:     "night",
		Priority: 2,
		Windows:  []filter.Window{{Days: []time.Weekday{time.Monday}, Start: 21 * 60, End: 7 * 60}},
	}
	weekend := filter.ScheduleSpec{
		Name:     "weekend",
		Priority: 5,
		Windows: []filter.Window{
			{Days: []time.Weekday{time.Saturday, time.Sunday}, Start: 0, End: 12 * 60},
			{Days: []time.Weekday{time.Sunday}, Start: 19 * 60, End: 21 * 60},
		},
	}
	for _, schedule := range []filter.ScheduleSpec{night, weekend} {
		require.NoError(t, s.SaveSchedule(ctx, schedule))
	}

	// Saving again replaces the stored windows and priority.
	weekend.Priority = 7
	require.NoError(t, s.SaveSchedule(ctx, weekend))

	got, err := s.Schedules(ctx)
	require.NoError(t, err)
	require.Equal(t, []filter.ScheduleSpec{night, weekend}, got)

	games := filter.RuleSpec{ID: "custom:1", Source: filter.Source{ID: "custom", Name: "custom"}, Kind: filter.MatchExact, Domain: mustDomain(t, "games.example"), Schedule: "night", Client: "tablet", Action: filter.ActionBlock}
	_, err = s.SaveRule(ctx, store.Rule{
		Kind:     filter.MatchExact,
		Domain:   mustDomain(t, "games.example"),
		Schedule: "night",
		Client:   "tablet",
		Action:   filter.ActionBlock,
	})
	require.NoError(t, err)
	rules, err := s.Rules(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Rule{{
		ID:       1,
		Kind:     filter.MatchExact,
		Domain:   mustDomain(t, "games.example"),
		Schedule: "night",
		Client:   "tablet",
		Action:   filter.ActionBlock,
	}}, rules)

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
	require.Equal(t, []filter.ScheduleSpec{night, weekend}, cfg.Schedules)
	require.Equal(t, []filter.RuleSpec{games}, cfg.Rules)

	set, err := filter.Compile(filter.Config{
		Rules:     cfg.Rules,
		Profiles:  cfg.Profiles,
		Clients:   cfg.ClientSpecs(),
		Schedules: cfg.Schedules,
		Default:   cfg.Default,
	})
	require.NoError(t, err)

	monday22 := time.Date(2026, 9, 21, 22, 0, 0, 0, time.UTC)
	mondayNoon := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	require.Equal(t, filter.ActionBlock, set.Decide(mustDomain(t, "games.example"), "tablet", netip.Addr{}, monday22).Action)
	require.Equal(t, filter.ActionAllow, set.Decide(mustDomain(t, "games.example"), "tablet", netip.Addr{}, mondayNoon).Action)
	require.Equal(t, filter.ActionAllow, set.Decide(mustDomain(t, "games.example"), "laptop", netip.Addr{}, monday22).Action)
}

func TestAConfigWithAnUnknownScheduleIsInvalid(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	_, err := s.SaveRule(ctx, store.Rule{
		Kind:     filter.MatchExact,
		Domain:   mustDomain(t, "games.example"),
		Schedule: "ghost",
		Action:   filter.ActionBlock,
	})
	require.NoError(t, err)
	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Error(t, cfg.Validate())
}

func mustDomain(t testing.TB, name string) filter.Domain {
	t.Helper()
	domain, err := filter.ParseDomain(name)
	require.NoError(t, err)
	return domain
}

func TestRulesRoundTripTheNewMatchKinds(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	stored := []store.Rule{
		{Kind: filter.MatchWildcard, Pattern: "*.ads.example", Action: filter.ActionBlock},
		{Kind: filter.MatchRegex, Pattern: `^x[0-9]+\.example$`, Action: filter.ActionAllow},
		{Kind: filter.MatchCIDR, Network: mustNetwork(t, "10.4.1.2/16"), Action: filter.ActionBlock},
	}
	for i := range stored {
		id, err := s.SaveRule(ctx, stored[i])
		require.NoError(t, err)
		stored[i].ID = id
	}

	got, err := s.Rules(ctx)
	require.NoError(t, err)
	require.Equal(t, stored, got)

	specs := make([]filter.RuleSpec, 0, len(got))
	for _, rule := range got {
		specs = append(specs, rule.Spec())
	}
	set, err := filter.Compile(filter.Config{
		Rules:    specs,
		Profiles: []filter.ProfileSpec{{ID: "default"}},
		Default:  "default",
	})
	require.NoError(t, err)
	require.Equal(t, filter.ActionBlock, set.Decide(mustDomain(t, "srv.ads.example"), "", netip.Addr{}, time.Now()).Action)
	require.Equal(t, filter.ActionAllow, set.Decide(mustDomain(t, "x9.example"), "", netip.Addr{}, time.Now()).Action)
	require.Equal(t, filter.ActionBlock, set.Decide(mustDomain(t, "example.com"), "", netip.MustParseAddr("10.4.9.9"), time.Now()).Action)
}

func mustNetwork(t *testing.T, raw string) netip.Prefix {
	t.Helper()
	network, err := filter.ParseNetwork(raw)
	require.NoError(t, err)
	return network
}
