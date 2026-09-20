package dhcp

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Pool is the range of addresses one server may hand out, inside the network it
// serves.
type Pool struct {
	Network netip.Prefix
	First   netip.Addr
	Last    netip.Addr
}

// ParseRange reads a range such as "10.9.9.100-10.9.9.200" and the netmask of
// the network it belongs to, written as a prefix length ("24") or a dotted mask
// ("255.255.255.0"). The range must sit inside the network the mask describes,
// because an address outside it is one the clients cannot reach the server on.
func ParseRange(raw, mask string) (Pool, error) {
	start, end, found := strings.Cut(raw, "-")
	if !found {
		return Pool{}, fmt.Errorf("dhcp: range %q is not start-end", raw)
	}
	first, err := parseMaskedIPv4(start)
	if err != nil {
		return Pool{}, fmt.Errorf("dhcp: range %q: %w", raw, err)
	}
	last, err := parseMaskedIPv4(end)
	if err != nil {
		return Pool{}, fmt.Errorf("dhcp: range %q: %w", raw, err)
	}
	if first.Compare(last) > 0 {
		return Pool{}, fmt.Errorf("dhcp: range %q ends before it starts", raw)
	}

	bits, err := parseMask(mask)
	if err != nil {
		return Pool{}, err
	}
	network := netip.PrefixFrom(first, bits).Masked()
	if !network.Contains(last) {
		return Pool{}, fmt.Errorf("dhcp: range %q is not inside %s", raw, network)
	}
	if size := int(last.As4()[3]) - int(first.As4()[3]) + 1; size > maxPoolSize {
		return Pool{}, fmt.Errorf("dhcp: range %q holds %d addresses, more than %d", raw, size, maxPoolSize)
	}
	return Pool{Network: network, First: first, Last: last}, nil
}

// maxPoolSize bounds the walk the allocator may make looking for a free
// address, so a typo cannot turn one discover into a long scan.
const maxPoolSize = 4096

func parseMaskedIPv4(raw string) (netip.Addr, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return netip.Addr{}, err
	}
	if !address.Is4() {
		return netip.Addr{}, fmt.Errorf("%s is not an IPv4 address", raw)
	}
	return address, nil
}

func parseMask(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("dhcp: a pool needs a netmask")
	}
	if bits, err := netip.ParsePrefix("0.0.0.0/" + raw); err == nil {
		return bits.Bits(), nil
	}
	mask := net.ParseIP(raw)
	if mask == nil {
		return 0, fmt.Errorf("dhcp: netmask %q is not an address", raw)
	}
	ipv4 := mask.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("dhcp: netmask %q is not IPv4", raw)
	}
	ones, bits := net.IPMask(ipv4).Size()
	if bits != 32 || ones == 0 {
		return 0, fmt.Errorf("dhcp: netmask %q is not contiguous", raw)
	}
	return ones, nil
}

// Addresses walks the pool from its first address to its last, stopping when
// the callback returns false. It is how the allocator looks for a free address.
func (p Pool) Addresses(each func(netip.Addr) bool) {
	for address := p.First; address.Compare(p.Last) <= 0; address = address.Next() {
		if !each(address) {
			return
		}
		if !address.IsValid() {
			return
		}
	}
}

// Contains reports whether an address is one the pool may hand out.
func (p Pool) Contains(address netip.Addr) bool {
	if !address.Is4() {
		return false
	}
	return address.Compare(p.First) >= 0 && address.Compare(p.Last) <= 0
}
