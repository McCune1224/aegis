package client_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/client"
	"aegis/internal/filter"
)

func TestKeyReturnsTheIdentityAnAddressCarries(t *testing.T) {
	tablet := netip.MustParseAddr("10.9.9.2")

	resolver, err := client.New([]client.Spec{{Key: "tablet", Addresses: []netip.Addr{tablet}}})
	require.NoError(t, err)

	require.Equal(t, filter.ClientKey("tablet"), resolver.Key(tablet))
	require.Equal(t, filter.ClientKey(""), resolver.Key(netip.MustParseAddr("10.9.9.9")))
}

func TestKeyMapsAV4MappedV6AddressToTheSameIdentity(t *testing.T) {
	resolver, err := client.New([]client.Spec{{
		Key:       "tablet",
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.2")},
	}})
	require.NoError(t, err)

	require.Equal(t, filter.ClientKey("tablet"), resolver.Key(netip.MustParseAddr("::ffff:10.9.9.2")))
}

func TestOneAddressCannotCarryTwoIdentities(t *testing.T) {
	address := netip.MustParseAddr("10.9.9.2")

	_, err := client.New([]client.Spec{
		{Key: "tablet", Addresses: []netip.Addr{address}},
		{Key: "laptop", Addresses: []netip.Addr{address}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "claimed by both")
}

func TestASpecNeedsAKey(t *testing.T) {
	_, err := client.New([]client.Spec{{
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.2")},
	}})

	require.Error(t, err)
}

func TestNoSpecsLeavesEveryAddressUnidentified(t *testing.T) {
	resolver, err := client.New(nil)
	require.NoError(t, err)

	require.Equal(t, filter.ClientKey(""), resolver.Key(netip.MustParseAddr("10.9.9.2")))
}
