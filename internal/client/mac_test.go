package client_test

import (
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/client"
	"aegis/internal/filter"
)

func mac(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	parsed, err := net.ParseMAC(value)
	require.NoError(t, err)
	return parsed
}

func TestAMACSelectorCarriesIdentityWithNoAddress(t *testing.T) {
	resolver, err := client.New([]client.Spec{{
		Key: "tablet",
		MACs: []net.HardwareAddr{
			mac(t, "aa:bb:cc:dd:ee:01"),
		},
	}})
	require.NoError(t, err)

	require.Equal(t, filter.ClientKey("tablet"), resolver.Select(client.Selector{MAC: mac(t, "aa:bb:cc:dd:ee:01")}))
	require.Empty(t, resolver.Select(client.Selector{MAC: mac(t, "aa:bb:cc:dd:ee:02")}))
}

func TestAnAddressBeatsAMACAndAMACBeatsAPrefix(t *testing.T) {
	resolver, err := client.New([]client.Spec{
		{Key: "from-mac", MACs: []net.HardwareAddr{mac(t, "aa:bb:cc:dd:ee:01")}},
		{Key: "from-prefix", Prefixes: []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}},
		{Key: "from-address", Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.7")}},
	})
	require.NoError(t, err)

	onAddress := client.Selector{Address: netip.MustParseAddr("10.9.9.7"), MAC: mac(t, "aa:bb:cc:dd:ee:01")}
	require.Equal(t, filter.ClientKey("from-address"), resolver.Select(onAddress))

	onMAC := client.Selector{Address: netip.MustParseAddr("10.9.9.9"), MAC: mac(t, "aa:bb:cc:dd:ee:01")}
	require.Equal(t, filter.ClientKey("from-mac"), resolver.Select(onMAC))

	onPrefix := client.Selector{Address: netip.MustParseAddr("10.9.9.9"), MAC: mac(t, "aa:bb:cc:dd:ee:ff")}
	require.Equal(t, filter.ClientKey("from-prefix"), resolver.Select(onPrefix))
}

func TestTheSameMACOnTwoIdentitiesIsRefused(t *testing.T) {
	_, err := client.New([]client.Spec{
		{Key: "tablet", MACs: []net.HardwareAddr{mac(t, "aa:bb:cc:dd:ee:01")}},
		{Key: "phone", MACs: []net.HardwareAddr{mac(t, "AA:BB:CC:DD:EE:01")}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "aa:bb:cc:dd:ee:01")
}

func TestKeyLeavesTheAddressLookupAlone(t *testing.T) {
	resolver, err := client.New([]client.Spec{{
		Key:       "tablet",
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.7")},
		MACs:      []net.HardwareAddr{mac(t, "aa:bb:cc:dd:ee:01")},
	}})
	require.NoError(t, err)

	require.Equal(t, filter.ClientKey("tablet"), resolver.Key(netip.MustParseAddr("10.9.9.7")))
	require.Empty(t, resolver.Key(netip.MustParseAddr("10.9.9.8")))
}
