package client

import (
	"net"
	"net/netip"
	"sync"
)

// Dynamic is the address-to-hardware-address half of identity, which DHCP fills
// in as it grants leases. It is the reason a device that identifies itself by
// hardware address keeps its policy when a lease hands it a different address:
// the resolver asks here before it falls back to a prefix.
//
// It is written by the DHCP server and read on every query, so it is guarded.
type Dynamic struct {
	mu        sync.RWMutex
	byAddress map[netip.Addr]net.HardwareAddr
}

// NewDynamic returns an empty lease table.
func NewDynamic() *Dynamic {
	return &Dynamic{byAddress: make(map[netip.Addr]net.HardwareAddr)}
}

// Set records the device holding one address. A repeated set for the same
// address replaces the holder, which is what a device that lost its lease and
// came back looks like.
func (d *Dynamic) Set(address netip.Addr, hardware net.HardwareAddr) {
	if d == nil || NormalizeMAC(hardware) == "" || !address.IsValid() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.byAddress[address.Unmap()] = hardware
}

// Forget drops one address, which a release or an expiry does.
func (d *Dynamic) Forget(address netip.Addr) {
	if d == nil || !address.IsValid() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.byAddress, address.Unmap())
}

// MAC returns the device holding one address.
func (d *Dynamic) MAC(address netip.Addr) (net.HardwareAddr, bool) {
	if d == nil || !address.IsValid() {
		return nil, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	hardware, found := d.byAddress[address.Unmap()]
	return hardware, found
}

// UseLeases makes the resolver consult the addresses DHCP has handed out. It is
// set once, before the resolver is published, so reads never race the pointer.
func (r *Resolver) UseLeases(leases *Dynamic) { r.leases = leases }
