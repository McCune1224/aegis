// Package client resolves a source address to the identity that policy keys on.
// Identity lives here so the filter package never resolves one: it receives an
// address only as the payload its CIDR rules match on, and it never queries a
// lease table.
package client

import (
	"errors"
	"fmt"
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
	Prefixes  []netip.Prefix
}

// Resolver maps a source address to a client key.
type Resolver struct {
	byAddress map[netip.Addr]filter.ClientKey
	prefixes  []prefixSelector
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

	return &Resolver{byAddress: byAddress, prefixes: ordered}, nil
}

// Key returns the identity an address carries. An exact address beats a
// prefix, and among prefixes the longest match wins. An address nothing claims
// gives the empty key, which takes the default profile.
func (r *Resolver) Key(address netip.Addr) filter.ClientKey {
	normalized := address.Unmap()
	if key, exists := r.byAddress[normalized]; exists {
		return key
	}
	for _, selector := range r.prefixes {
		if selector.prefix.Contains(normalized) {
			return selector.key
		}
	}
	return ""
}
