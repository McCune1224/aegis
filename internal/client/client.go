// Package client resolves a source address to the identity that policy keys on.
// Identity lives here so the filter package never resolves one: it receives an
// address only as the payload its CIDR rules match on, and it never queries a
// lease table.
package client

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"

	"aegis/internal/filter"
)

// Spec binds one identity to the selectors that carry it. A client usually has
// several, because one device answers on more than one address and a whole
// network can share a policy.
type Spec struct {
	Key       filter.ClientKey
	Addresses []netip.Addr
	MACs      []net.HardwareAddr
	Prefixes  []netip.Prefix
}

// Selector is what one request carries for identity: the source address the
// packet came from, and the hardware address when the caller has one. Only a
// DHCP request knows both, which is why the address is not required.
type Selector struct {
	Address netip.Addr
	MAC     net.HardwareAddr
}

// Resolver maps a selector to a client key.
type Resolver struct {
	byAddress map[netip.Addr]filter.ClientKey
	byMAC     map[string]filter.ClientKey
	prefixes  []prefixSelector
	// leases is the addresses DHCP has handed out, which is where an address
	// that no selector claims finds its device.
	leases *Dynamic
}

type prefixSelector struct {
	prefix netip.Prefix
	key    filter.ClientKey
}

// New indexes the specs. A selector claimed by two identities is an error
// rather than a silent last-one-wins, because two policies fighting over one
// device is exactly the thing an operator cannot debug from the outside.
func New(specs []Spec) (*Resolver, error) {
	byAddress := make(map[netip.Addr]filter.ClientKey)
	byMAC := make(map[string]filter.ClientKey)
	byPrefix := make(map[netip.Prefix]filter.ClientKey)

	for _, spec := range specs {
		if spec.Key == "" {
			return nil, errors.New("client: a spec has no key")
		}
		for _, address := range spec.Addresses {
			normalized := address.Unmap()
			if existing, taken := byAddress[normalized]; taken {
				return nil, fmt.Errorf("client: address %s is claimed by both %q and %q", normalized, existing, spec.Key)
			}
			byAddress[normalized] = spec.Key
		}
		for _, hardware := range spec.MACs {
			normalized := NormalizeMAC(hardware)
			if normalized == "" {
				return nil, fmt.Errorf("client: %q has a hardware address that is not one", spec.Key)
			}
			if existing, taken := byMAC[normalized]; taken {
				return nil, fmt.Errorf("client: hardware address %s is claimed by both %q and %q", normalized, existing, spec.Key)
			}
			byMAC[normalized] = spec.Key
		}
		for _, prefix := range spec.Prefixes {
			masked := prefix.Masked()
			if existing, taken := byPrefix[masked]; taken {
				return nil, fmt.Errorf("client: prefix %s is claimed by both %q and %q", masked, existing, spec.Key)
			}
			byPrefix[masked] = spec.Key
		}
	}

	ordered := make([]prefixSelector, 0, len(byPrefix))
	for prefix, key := range byPrefix {
		ordered = append(ordered, prefixSelector{prefix: prefix, key: key})
	}
	// Longest prefix first, so the most specific network wins. Equal lengths
	// cannot both match one address, because a prefix is either contained in
	// another of the same length or identical to it, and identical is rejected.
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].prefix.Bits() > ordered[j].prefix.Bits()
	})

	return &Resolver{byAddress: byAddress, byMAC: byMAC, prefixes: ordered}, nil
}

// NormalizeMAC is the form hardware addresses are keyed on: lower case, one
// spelling, so a device that reports its address two ways is one device.
func NormalizeMAC(hardware net.HardwareAddr) string {
	if len(hardware) != 6 {
		return ""
	}
	return hardware.String()
}

// Key returns the identity an address carries.
func (r *Resolver) Key(address netip.Addr) filter.ClientKey {
	return r.Select(Selector{Address: address})
}

// Select returns the identity a request carries. An exact address beats a
// hardware address, which beats a prefix, so pinning one device to one policy
// does not depend on where it got its address from.
func (r *Resolver) Select(selector Selector) filter.ClientKey {
	if selector.Address.IsValid() {
		if key, exists := r.byAddress[selector.Address.Unmap()]; exists {
			return key
		}
	}
	if r.leases != nil {
		if hardware, held := r.leases.MAC(selector.Address); held {
			if key, exists := r.byMAC[NormalizeMAC(hardware)]; exists {
				return key
			}
		}
	}
	if key, exists := r.byMAC[NormalizeMAC(selector.MAC)]; exists {
		return key
	}
	if !selector.Address.IsValid() {
		return ""
	}
	normalized := selector.Address.Unmap()
	for _, candidate := range r.prefixes {
		if candidate.prefix.Contains(normalized) {
			return candidate.key
		}
	}
	return ""
}
