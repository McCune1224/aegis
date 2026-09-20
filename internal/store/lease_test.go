package store_test

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
)

func hardware(t *testing.T, raw string) net.HardwareAddr {
	t.Helper()
	parsed, err := net.ParseMAC(raw)
	require.NoError(t, err)
	return parsed
}

func TestALeaseRoundTripsWithTheClientItCarried(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "tablet", Profile: "kids"}))

	expires := time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC)
	require.NoError(t, s.SaveLease(ctx, store.Lease{
		Address:  netip.MustParseAddr("10.9.9.20"),
		MAC:      hardware(t, "aa:bb:cc:dd:ee:01"),
		Client:   "tablet",
		Hostname: "tablet",
		Expires:  expires,
	}))

	leases, err := s.Leases(ctx)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, netip.MustParseAddr("10.9.9.20"), leases[0].Address)
	require.Equal(t, hardware(t, "aa:bb:cc:dd:ee:01"), leases[0].MAC)
	require.Equal(t, filter.ClientKey("tablet"), leases[0].Client)
	require.Equal(t, "tablet", leases[0].Hostname)
	require.Equal(t, expires.UnixMilli(), leases[0].Expires.UnixMilli())
}

func TestSaveLeaseReplacesTheLeaseForAnAddress(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	address := netip.MustParseAddr("10.9.9.20")

	require.NoError(t, s.SaveLease(ctx, store.Lease{
		Address: address, MAC: hardware(t, "aa:bb:cc:dd:ee:01"), Expires: time.Now().Add(time.Hour),
	}))
	require.NoError(t, s.SaveLease(ctx, store.Lease{
		Address: address, MAC: hardware(t, "aa:bb:cc:dd:ee:02"), Hostname: "again", Expires: time.Now().Add(2 * time.Hour),
	}))

	leases, err := s.Leases(ctx)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, hardware(t, "aa:bb:cc:dd:ee:02"), leases[0].MAC)
	require.Equal(t, "again", leases[0].Hostname)
}

func TestExpireLeasesRemovesOnlyTheEndedOnes(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	require.NoError(t, s.SaveLease(ctx, store.Lease{
		Address: netip.MustParseAddr("10.9.9.20"), MAC: hardware(t, "aa:bb:cc:dd:ee:01"), Expires: now.Add(-time.Minute),
	}))
	require.NoError(t, s.SaveLease(ctx, store.Lease{
		Address: netip.MustParseAddr("10.9.9.21"), MAC: hardware(t, "aa:bb:cc:dd:ee:02"), Expires: now.Add(time.Minute),
	}))

	removed, err := s.ExpireLeases(ctx, now)
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	leases, err := s.Leases(ctx)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, netip.MustParseAddr("10.9.9.21"), leases[0].Address)
}

func TestDeletingAClientLeavesItsLeaseWithoutIdentity(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "tablet", Profile: "kids"}))
	require.NoError(t, s.SaveLease(ctx, store.Lease{
		Address: netip.MustParseAddr("10.9.9.20"), MAC: hardware(t, "aa:bb:cc:dd:ee:01"),
		Client: "tablet", Expires: time.Now().Add(time.Hour),
	}))

	require.NoError(t, s.DeleteClient(ctx, "tablet"))

	leases, err := s.Leases(ctx)
	require.NoError(t, err)
	require.Len(t, leases, 1, "the address is still held")
	require.Empty(t, leases[0].Client)
}

func TestADiscoveryKeepsItsFirstSightingAndMovesItsLast(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	first := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)
	last := first.Add(30 * time.Minute)

	require.NoError(t, s.RecordDiscovery(ctx, store.Discovery{
		MAC: hardware(t, "aa:bb:cc:dd:ee:09"), Address: netip.MustParseAddr("10.9.9.30"),
		Hostname: "phone", First: first, Last: first,
	}))
	require.NoError(t, s.RecordDiscovery(ctx, store.Discovery{
		MAC: hardware(t, "aa:bb:cc:dd:ee:09"), Address: netip.MustParseAddr("10.9.9.31"),
		Hostname: "phone", First: first, Last: last,
	}))

	discoveries, err := s.Discoveries(ctx)
	require.NoError(t, err)
	require.Len(t, discoveries, 1)
	require.Equal(t, netip.MustParseAddr("10.9.9.31"), discoveries[0].Address)
	require.Equal(t, first.UnixMilli(), discoveries[0].First.UnixMilli())
	require.Equal(t, last.UnixMilli(), discoveries[0].Last.UnixMilli())
}

func TestDeletingADiscoveryRemovesThePrompt(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.RecordDiscovery(ctx, store.Discovery{
		MAC: hardware(t, "aa:bb:cc:dd:ee:09"), Address: netip.MustParseAddr("10.9.9.30"), Last: time.Now(),
	}))

	require.NoError(t, s.DeleteDiscovery(ctx, hardware(t, "aa:bb:cc:dd:ee:09")))

	discoveries, err := s.Discoveries(ctx)
	require.NoError(t, err)
	require.Empty(t, discoveries)
}

func TestAClientsHardwareAddressReachesTheResolver(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SaveClient(ctx, store.Client{
		Key: "tablet", Profile: "kids", MACs: []net.HardwareAddr{hardware(t, "AA:BB:CC:DD:EE:01")},
	}))

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Clients, 1)
	require.Equal(t, []net.HardwareAddr{hardware(t, "aa:bb:cc:dd:ee:01")}, cfg.Clients[0].MACs)
	require.Equal(t, []net.HardwareAddr{hardware(t, "aa:bb:cc:dd:ee:01")}, cfg.Selectors()[0].MACs)
}
