// Package client resolves a source address to the identity that policy keys on.
// Identity lives here so the filter package never reads an address and never
// queries a lease table.
package client

import (
	"errors"
	"fmt"
	"net/netip"

	"aegis/internal/filter"
)

// Spec binds one identity to the addresses that carry it.
type Spec struct {
	Key       filter.ClientKey
	Addresses []netip.Addr
}

// Resolver maps a source address to a client key.
type Resolver struct {
	byAddress map[netip.Addr]filter.ClientKey
}

// New indexes the specs. Two specs claiming one address is an error rather than
// a silent last-one-wins, because a duplicated address means two policies are
// fighting and the operator needs to know which one won.
func New(specs []Spec) (*Resolver, error) {
	byAddress := make(map[netip.Addr]filter.ClientKey)
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
	}
	return &Resolver{byAddress: byAddress}, nil
}

// Key returns the identity an address carries. An address no spec claims gives
// the empty key, which takes the default profile.
func (r *Resolver) Key(address netip.Addr) filter.ClientKey {
	return r.byAddress[address.Unmap()]
}
