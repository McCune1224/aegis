package client_test

import (
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/client"
	"aegis/internal/filter"
)

func TestALeaseCarriesHardwareIdentityToTheAddressLookup(t *testing.T) {
	leases := client.NewDynamic()
	resolver, err := client.New([]client.Spec{{
		Key:  "tablet",
		MACs: []net.HardwareAddr{mac(t, "aa:bb:cc:dd:ee:01")},
	}})
	require.NoError(t, err)
	resolver.UseLeases(leases)

	address := netip.MustParseAddr("10.9.9.20")
	require.Empty(t, resolver.Key(address), "nothing claims the address yet")

	leases.Set(address, mac(t, "aa:bb:cc:dd:ee:01"))
	require.Equal(t, filter.ClientKey("tablet"), resolver.Key(address))

	leases.Forget(address)
	require.Empty(t, resolver.Key(address))
}

func TestAConfiguredAddressBeatsALease(t *testing.T) {
	leases := client.NewDynamic()
	resolver, err := client.New([]client.Spec{
		{Key: "pinned", Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.20")}},
		{Key: "tablet", MACs: []net.HardwareAddr{mac(t, "aa:bb:cc:dd:ee:01")}},
	})
	require.NoError(t, err)
	resolver.UseLeases(leases)

	address := netip.MustParseAddr("10.9.9.20")
	leases.Set(address, mac(t, "aa:bb:cc:dd:ee:01"))
	require.Equal(t, filter.ClientKey("pinned"), resolver.Key(address), "an operator's pin is not overruled by a lease")
}

func TestALeaseForAnUnclaimedDeviceLeavesTheAddressUnknown(t *testing.T) {
	leases := client.NewDynamic()
	resolver, err := client.New([]client.Spec{{
		Key:       "guests",
		Prefixes:  []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")},
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.7")},
	}})
	require.NoError(t, err)
	resolver.UseLeases(leases)

	leases.Set(netip.MustParseAddr("10.9.9.30"), mac(t, "aa:bb:cc:dd:ee:09"))
	require.Equal(t, filter.ClientKey("guests"), resolver.Key(netip.MustParseAddr("10.9.9.30")), "the prefix still catches it")
	require.Equal(t, filter.ClientKey("guests"), resolver.Key(netip.MustParseAddr("10.9.9.31")))
}

func TestTheLeaseTableIsSafeToReadAndWriteAtOnce(t *testing.T) {
	leases := client.NewDynamic()

	var group sync.WaitGroup
	for i := range 32 {
		group.Add(2)
		go func() {
			defer group.Done()
			leases.Set(netip.AddrFrom4([4]byte{10, 9, 9, byte(i)}), mac(t, "aa:bb:cc:dd:ee:01"))
		}()
		go func() {
			defer group.Done()
			leases.MAC(netip.AddrFrom4([4]byte{10, 9, 9, byte(i)}))
		}()
	}
	group.Wait()
}
