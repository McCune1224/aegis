package runtime_test

import (
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

// tabletMAC is the hardware address the tests claim, and the one a lease hands
// a profile to.
var tabletMAC = net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}

// TestALeaseHandsAProfileToTheAddressItGranted is the done-when for #27: the
// device has no address selector, and the policy arrives through the hardware
// address the DHCP server recorded.
func TestALeaseHandsAProfileToTheAddressItGranted(t *testing.T) {
	s := openStore(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.Refused)}))
	require.NoError(t, s.SaveClient(ctx, store.Client{
		Key:     "tablet",
		Profile: "kids",
		MACs:    []net.HardwareAddr{tabletMAC},
	}))

	leases := client.NewDynamic()
	rt := runtime.New(s, blockedLists(t), quietLogger(), runtime.WithLeases(leases))
	require.NoError(t, rt.Reload(ctx))

	granted := netip.MustParseAddr("10.9.9.50")
	require.Equal(t, filter.NXDomain, rt.Decide(blockedName, granted).Policy.Mode,
		"nothing claims the address yet, so the default profile answers")

	leases.Set(granted, tabletMAC)

	verdict := rt.Decide(blockedName, granted)
	require.Equal(t, filter.ActionBlock, verdict.Action)
	require.Equal(t, filter.Refused, verdict.Policy.Mode,
		"the address the lease granted carries the hardware address's profile")

	require.Equal(t, filter.NXDomain, rt.Decide(blockedName, netip.MustParseAddr("10.9.9.51")).Policy.Mode,
		"a device with no lease and no selector keeps the default")
}

// TestAReleaseHandsTheAddressBackToTheDefault covers the other direction: an
// address that stops being leased stops carrying the policy.
func TestAReleaseHandsTheAddressBackToTheDefault(t *testing.T) {
	s := openStore(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.Refused)}))
	require.NoError(t, s.SaveClient(ctx, store.Client{
		Key:     "tablet",
		Profile: "kids",
		MACs:    []net.HardwareAddr{tabletMAC},
	}))

	leases := client.NewDynamic()
	rt := runtime.New(s, blockedLists(t), quietLogger(), runtime.WithLeases(leases))
	require.NoError(t, rt.Reload(ctx))

	granted := netip.MustParseAddr("10.9.9.50")
	leases.Set(granted, tabletMAC)
	require.Equal(t, filter.Refused, rt.Decide(blockedName, granted).Policy.Mode)

	leases.Forget(granted)
	require.Equal(t, filter.NXDomain, rt.Decide(blockedName, granted).Policy.Mode)
}
