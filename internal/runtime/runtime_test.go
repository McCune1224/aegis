package runtime_test

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

var tablet = netip.MustParseAddr("10.9.9.2")

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func modePtr(mode filter.BlockingMode) *filter.BlockingMode { return &mode }

// blockedName is the name every rule in these tests blocks. A package-level var
// is the only way to build a Domain, because the field holding its name is
// private to the filter package.
var blockedName = func() filter.Domain {
	domain, err := filter.ParseDomain("ads.example.com")
	if err != nil {
		panic(err)
	}
	return domain
}()

func blockedAds() filter.RuleSpec {
	return filter.RuleSpec{
		ID:     "block-ads",
		Source: filter.Source{ID: "test", Name: "Test"},
		Kind:   filter.MatchSubdomains,
		Domain: blockedName,
		Action: filter.ActionBlock,
	}
}

// configuredStore returns a store holding one profile and one client bound to
// the tablet address. The profile starts as refused, which the tests that change
// it override.
func configuredStore(t *testing.T) *store.Store {
	t.Helper()
	s := openStore(t)
	ctx := t.Context()

	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.Refused)}))
	require.NoError(t, s.SaveClient(ctx, store.Client{
		Key:       "tablet",
		Profile:   "kids",
		Addresses: []netip.Addr{tablet},
	}))
	require.NoError(t, s.SetDefaultProfile(ctx, "kids"))
	return s
}

func TestNothingIsBlockedBeforeTheFirstReload(t *testing.T) {
	rt := runtime.New(openStore(t), []filter.RuleSpec{blockedAds()})

	got := rt.Decide(blockedName, tablet)

	require.Equal(t, filter.ActionAllow, got.Action)
}

func TestReloadPublishesTheStoredConfiguration(t *testing.T) {
	rt := runtime.New(configuredStore(t), []filter.RuleSpec{blockedAds()})
	require.NoError(t, rt.Reload(t.Context()))

	got := rt.Decide(blockedName, tablet)

	require.Equal(t, filter.ActionBlock, got.Action)
	require.Equal(t, filter.Refused, got.Policy.Mode)
	require.Equal(t, 1, rt.Size())
}

func TestAStoredChangeReachesARunningRuntime(t *testing.T) {
	ctx := t.Context()
	s := configuredStore(t)
	rt := runtime.New(s, []filter.RuleSpec{blockedAds()})
	require.NoError(t, rt.Reload(ctx))
	require.Equal(t, filter.Refused, rt.Decide(blockedName, tablet).Policy.Mode)

	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.NullAddress)}))
	require.NoError(t, rt.Reload(ctx))

	require.Equal(t, filter.NullAddress, rt.Decide(blockedName, tablet).Policy.Mode)
}

func TestABadConfigurationLeavesThePreviousGenerationServing(t *testing.T) {
	ctx := t.Context()
	s := configuredStore(t)
	rt := runtime.New(s, []filter.RuleSpec{blockedAds()})
	require.NoError(t, rt.Reload(ctx))

	// A cycle between two stored profiles compiles to nothing. The database
	// allows it because both rows exist and the foreign key only checks that.
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "a", Extends: "default", Mode: modePtr(filter.NXDomain)}))
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "b", Extends: "a", Mode: modePtr(filter.NXDomain)}))
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "a", Extends: "b", Mode: modePtr(filter.NXDomain)}))

	err := rt.Reload(ctx)

	require.Error(t, err)
	require.Contains(t, err.Error(), "extends itself")
	require.Equal(t, filter.Refused, rt.Decide(blockedName, tablet).Policy.Mode,
		"a failed reload must not take the resolver down")
}

func TestAReloadIsSafeWhileQueriesRun(t *testing.T) {
	ctx := t.Context()
	s := configuredStore(t)
	rt := runtime.New(s, []filter.RuleSpec{blockedAds()})
	require.NoError(t, rt.Reload(ctx))

	name := blockedName
	stop := make(chan struct{})
	torn := make(chan string, 8)

	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				got := rt.Decide(name, tablet)
				// Every generation this store compiles blocks the name and gives
				// the tablet the refused policy, so the only way to see anything
				// else is to read two generations in one call.
				if got.Action != filter.ActionBlock || got.Policy.Mode != filter.Refused {
					torn <- fmt.Sprintf("action=%d mode=%d", got.Action, got.Policy.Mode)
					return
				}
			}
		}()
	}

	for range 200 {
		require.NoError(t, rt.Reload(ctx))
	}

	close(stop)
	readers.Wait()
	close(torn)

	for msg := range torn {
		t.Errorf("a query read a torn generation: %s", msg)
	}
}
