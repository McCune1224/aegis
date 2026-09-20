package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"aegis/internal/config"
	"aegis/internal/dhcp"
)

// buildDHCPConfig reads the DHCP flags into a server configuration. An empty
// range turns the server off, which is how a deployment that already has a DHCP
// server keeps working: Aegis reads nothing and serves nothing.
func buildDHCPConfig(cfg config.Config) (dhcp.Config, error) {
	if cfg.DHCPRange == "" {
		if cfg.DHCPAddress != "" {
			return dhcp.Config{}, errors.New("dhcp-address needs dhcp-range")
		}
		return dhcp.Config{}, nil
	}
	if cfg.DHCPAddress == "" {
		return dhcp.Config{}, errors.New("dhcp-range needs dhcp-address")
	}

	pool, err := dhcp.ParseRange(cfg.DHCPRange, cfg.DHCPNetmask)
	if err != nil {
		return dhcp.Config{}, err
	}

	leaseTime, err := time.ParseDuration(cfg.DHCPLeaseTime)
	if err != nil {
		return dhcp.Config{}, fmt.Errorf("dhcp-lease-time: %w", err)
	}
	if leaseTime <= 0 {
		return dhcp.Config{}, errors.New("dhcp-lease-time must be positive")
	}

	serverIP, err := parseOptionalAddress(cfg.DHCPServerIP, "dhcp-server-ip")
	if err != nil {
		return dhcp.Config{}, err
	}
	if !serverIP.IsValid() {
		// The first usable address of the network is the router in every
		// default home layout, and the operator can name another.
		serverIP = pool.Network.Addr().Next()
	}
	if !pool.Network.Contains(serverIP) {
		return dhcp.Config{}, fmt.Errorf("dhcp-server-ip %s is not inside %s", serverIP, pool.Network)
	}

	router, err := parseOptionalAddress(cfg.DHCPRouter, "dhcp-router")
	if err != nil {
		return dhcp.Config{}, err
	}
	if !router.IsValid() {
		router = serverIP
	}

	resolvers, err := dhcpResolvers(cfg, serverIP)
	if err != nil {
		return dhcp.Config{}, err
	}

	return dhcp.Config{
		Address:   cfg.DHCPAddress,
		Pool:      pool,
		ServerIP:  serverIP,
		Router:    router,
		DNS:       resolvers,
		LeaseTime: leaseTime,
	}, nil
}

// dhcpResolvers is what clients are told to resolve with: the operator's list,
// or the address the DNS listener answers on, which is the point of running the
// two together.
func dhcpResolvers(cfg config.Config, serverIP netip.Addr) ([]netip.Addr, error) {
	if len(cfg.DHCPDNS) > 0 {
		resolvers := make([]netip.Addr, 0, len(cfg.DHCPDNS))
		for _, raw := range cfg.DHCPDNS {
			address, err := parseOptionalAddress(raw, "dhcp-dns")
			if err != nil {
				return nil, err
			}
			if !address.IsValid() {
				return nil, fmt.Errorf("dhcp-dns %q is not an address", raw)
			}
			resolvers = append(resolvers, address)
		}
		return resolvers, nil
	}

	host, _, err := net.SplitHostPort(cfg.DNSAddress)
	if err != nil {
		return nil, fmt.Errorf("dns-address: %w", err)
	}
	address, err := parseOptionalAddress(host, "dns-address")
	if err != nil {
		return nil, err
	}
	if !address.IsValid() || address.IsUnspecified() {
		// The listener is on every interface, so the only address a client can
		// be told about is the one the server itself answers on.
		address = serverIP
	}
	return []netip.Addr{address}, nil
}
