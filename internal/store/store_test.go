package store_test

import (
	"net/netip"
	"path/filepath"
	"testing"

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
	require.Equal(t, filter.Refused, set.Decide(mustDomain(t, "example.com"), "tablet").Policy.Mode)

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

func mustDomain(t *testing.T, name string) filter.Domain {
	t.Helper()
	domain, err := filter.ParseDomain(name)
	require.NoError(t, err)
	return domain
}
