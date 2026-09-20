package runtime

import "net/netip"

// gate carries the listener's two stored client sets. A disallowed address is
// refused even when a wider allowed prefix would take it, and a non-empty
// allowed set serves only the addresses it lists. It is immutable, so one
// generation can answer while the next loads.
type gate struct {
	allowed    []netip.Prefix
	disallowed []netip.Prefix
}

// Allows normalizes the address the same way client.Resolver does, so a
// mapped IPv4 address matches the stored IPv4 row on every transport.
func (g gate) Allows(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range g.disallowed {
		if prefix.Contains(address) {
			return false
		}
	}
	if len(g.allowed) == 0 {
		return true
	}
	for _, prefix := range g.allowed {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
