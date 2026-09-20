package main

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/config"
)

func TestDHCPFlagsBecomeAPoolAndAJobForTheResolver(t *testing.T) {
	cfg := config.Config{
		DHCPAddress:   "127.0.0.1:6767",
		DHCPRange:     "10.9.9.100-10.9.9.200",
		DHCPNetmask:   "255.255.255.0",
		DHCPLeaseTime: "30m",
		DNSAddress:    "10.9.9.5:53",
	}

	got, err := buildDHCPConfig(cfg)
	require.NoError(t, err)

	require.Equal(t, "127.0.0.1:6767", got.Address)
	require.Equal(t, netip.MustParseAddr("10.9.9.100"), got.Pool.First)
	require.Equal(t, netip.MustParseAddr("10.9.9.200"), got.Pool.Last)
	require.Equal(t, netip.MustParseAddr("10.9.9.1"), got.ServerIP, "the first address of the network is the default")
	require.Equal(t, netip.MustParseAddr("10.9.9.1"), got.Router)
	require.Equal(t, []netip.Addr{netip.MustParseAddr("10.9.9.5")}, got.DNS, "clients are told to ask the DNS listener")
	require.Equal(t, 30*time.Minute, got.LeaseTime)
}

func TestDHCPIsOffUntilARangeIsGiven(t *testing.T) {
	got, err := buildDHCPConfig(config.Config{DNSAddress: "127.0.0.1:53"})
	require.NoError(t, err)
	require.False(t, got.Pool.First.IsValid())

	_, err = buildDHCPConfig(config.Config{DHCPAddress: "127.0.0.1:6767"})
	require.ErrorContains(t, err, "dhcp-range")

	_, err = buildDHCPConfig(config.Config{DHCPRange: "10.9.9.100-10.9.9.200"})
	require.ErrorContains(t, err, "dhcp-address")
}

func TestDHCPSaysWhatItCannotUse(t *testing.T) {
	base := config.Config{
		DHCPAddress:   "127.0.0.1:6767",
		DHCPRange:     "10.9.9.100-10.9.9.200",
		DHCPNetmask:   "255.255.255.0",
		DHCPLeaseTime: "12h",
		DNSAddress:    "127.0.0.1:53",
	}

	broken := base
	broken.DHCPLeaseTime = "soon"
	_, err := buildDHCPConfig(broken)
	require.ErrorContains(t, err, "dhcp-lease-time")

	broken = base
	broken.DHCPLeaseTime = "0s"
	_, err = buildDHCPConfig(broken)
	require.ErrorContains(t, err, "positive")

	broken = base
	broken.DHCPServerIP = "10.9.10.1"
	_, err = buildDHCPConfig(broken)
	require.ErrorContains(t, err, "not inside")

	broken = base
	broken.DHCPDNS = []string{"not-an-address"}
	_, err = buildDHCPConfig(broken)
	require.ErrorContains(t, err, "dhcp-dns")

	broken = base
	broken.DHCPRange = "10.9.9.200-10.9.9.100"
	_, err = buildDHCPConfig(broken)
	require.ErrorContains(t, err, "ends before it starts")
}

func TestAClientFacingAddressIsUsedWhenTheListenerIsOnEveryInterface(t *testing.T) {
	got, err := buildDHCPConfig(config.Config{
		DHCPAddress:   "127.0.0.1:6767",
		DHCPRange:     "10.9.9.100-10.9.9.200",
		DHCPNetmask:   "255.255.255.0",
		DHCPLeaseTime: "12h",
		DNSAddress:    "0.0.0.0:53",
		DHCPServerIP:  "10.9.9.7",
	})
	require.NoError(t, err)
	require.Equal(t, []netip.Addr{netip.MustParseAddr("10.9.9.7")}, got.DNS, "a wildcard listener names no address a client can use")
}
