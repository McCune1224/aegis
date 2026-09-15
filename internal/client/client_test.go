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

func TestTheLongerPrefixWins(t *testing.T) {
	resolver, err := client.New([]client.Spec{
		{Key: "home", Prefixes: []netip.Prefix{netip.MustParsePrefix("10.9.0.0/16")}},
		{Key: "guest", Prefixes: []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}},
	})
	require.NoError(t, err)

	require.Equal(t, filter.ClientKey("guest"), resolver.Key(netip.MustParseAddr("10.9.9.10")))
	require.Equal(t, filter.ClientKey("home"), resolver.Key(netip.MustParseAddr("10.9.8.10")))
}

func TestAnExactAddressBeatsAPrefix(t *testing.T) {
	resolver, err := client.New([]client.Spec{
		{Key: "guest", Prefixes: []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}},
		{Key: "tablet", Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.10")}},
	})
	require.NoError(t, err)

	require.Equal(t, filter.ClientKey("tablet"), resolver.Key(netip.MustParseAddr("10.9.9.10")))
	require.Equal(t, filter.ClientKey("guest"), resolver.Key(netip.MustParseAddr("10.9.9.11")))
}

func TestAnAddressOutsideEveryPrefixIsUnidentified(t *testing.T) {
	resolver, err := client.New([]client.Spec{
		{Key: "guest", Prefixes: []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}},
	})
	require.NoError(t, err)

	require.Equal(t, filter.ClientKey(""), resolver.Key(netip.MustParseAddr("192.0.2.7")))
}

func TestOnePrefixCannotCarryTwoIdentities(t *testing.T) {
	prefix := netip.MustParsePrefix("10.9.9.0/24")

	_, err := client.New([]client.Spec{
		{Key: "guest", Prefixes: []netip.Prefix{prefix}},
		{Key: "tablet", Prefixes: []netip.Prefix{prefix}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "claimed by both")
}
