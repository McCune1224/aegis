package store

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/store/storedb"
)

// Lease is one address a DHCP server handed to one device for a while. It is
// not configuration: it expires, and the sweep removes it. Client is the
// identity the lease was written under, and is empty when no client record
// claimed the device.
type Lease struct {
	Address  netip.Addr
	MAC      net.HardwareAddr
	Client   filter.ClientKey
	Hostname string
	Expires  time.Time
}

// Discovery is a device that served itself an address no client record claims.
// It is what the operator is asked about, and it stays until they answer.
type Discovery struct {
	MAC      net.HardwareAddr
	Address  netip.Addr
	Hostname string
	First    time.Time
	Last     time.Time
}

// Leases returns every stored lease, expired ones included; the sweep is a
// separate call so a reader can see what is about to go.
func (s *Store) Leases(ctx context.Context) ([]Lease, error) {
	rows, err := s.queries.ListLeases(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: leases: %w", err)
	}

	leases := make([]Lease, 0, len(rows))
	for _, row := range rows {
		lease, err := leaseFrom(row)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

func leaseFrom(row storedb.Lease) (Lease, error) {
	address, err := netip.ParseAddr(row.Address)
	if err != nil {
		return Lease{}, fmt.Errorf("store: lease %s: %w", row.Address, err)
	}
	hardware, err := net.ParseMAC(row.Mac)
	if err != nil {
		return Lease{}, fmt.Errorf("store: lease %s: %w", row.Address, err)
	}
	lease := Lease{
		Address:  address,
		MAC:      hardware,
		Hostname: row.Hostname,
		Expires:  time.UnixMilli(row.Expires),
	}
	if row.Client != nil {
		lease.Client = filter.ClientKey(*row.Client)
	}
	return lease, nil
}

// SaveLease inserts or replaces the lease for one address, so a device that
// renews keeps one row rather than one per request.
func (s *Store) SaveLease(ctx context.Context, lease Lease) error {
	if err := validateLease(lease); err != nil {
		return err
	}

	params := storedb.UpsertLeaseParams{
		Address:  lease.Address.Unmap().String(),
		Mac:      client.NormalizeMAC(lease.MAC),
		Hostname: lease.Hostname,
		Expires:  lease.Expires.UnixMilli(),
	}
	if lease.Client != "" {
		key := string(lease.Client)
		params.Client = &key
	}
	if err := s.queries.UpsertLease(ctx, params); err != nil {
		return fmt.Errorf("store: save lease %s: %w", lease.Address, err)
	}
	return nil
}

// DeleteLease removes the lease for one address, which is what a client
// releasing or declining an address does.
func (s *Store) DeleteLease(ctx context.Context, address netip.Addr) error {
	if err := s.queries.DeleteLease(ctx, address.Unmap().String()); err != nil {
		return fmt.Errorf("store: delete lease %s: %w", address, err)
	}
	return nil
}

// ExpireLeases removes every lease that ended at or before now and reports how
// many went, so the sweep can say whether it did anything.
func (s *Store) ExpireLeases(ctx context.Context, now time.Time) (int64, error) {
	removed, err := s.queries.DeleteExpiredLeases(ctx, now.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("store: expire leases: %w", err)
	}
	return removed, nil
}

func validateLease(lease Lease) error {
	if !lease.Address.IsValid() {
		return errors.New("store: a lease needs an address")
	}
	if client.NormalizeMAC(lease.MAC) == "" {
		return fmt.Errorf("store: lease %s has no usable hardware address", lease.Address)
	}
	if lease.Expires.IsZero() {
		return fmt.Errorf("store: lease %s has no end", lease.Address)
	}
	return nil
}

// Discoveries returns every unclaimed device, most recently seen first.
func (s *Store) Discoveries(ctx context.Context) ([]Discovery, error) {
	rows, err := s.queries.ListDiscoveries(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: discoveries: %w", err)
	}

	discoveries := make([]Discovery, 0, len(rows))
	for _, row := range rows {
		address, err := netip.ParseAddr(row.Address)
		if err != nil {
			return nil, fmt.Errorf("store: discovery %s: %w", row.Mac, err)
		}
		hardware, err := net.ParseMAC(row.Mac)
		if err != nil {
			return nil, fmt.Errorf("store: discovery %s: %w", row.Mac, err)
		}
		discoveries = append(discoveries, Discovery{
			MAC:      hardware,
			Address:  address,
			Hostname: row.Hostname,
			First:    time.UnixMilli(row.First),
			Last:     time.UnixMilli(row.Last),
		})
	}
	return discoveries, nil
}

// RecordDiscovery notes a device that answered on an address no client record
// claims. The first sighting keeps its time, so the prompt can say how long the
// device has been around.
func (s *Store) RecordDiscovery(ctx context.Context, discovery Discovery) error {
	if client.NormalizeMAC(discovery.MAC) == "" {
		return errors.New("store: a discovery needs a hardware address")
	}
	if !discovery.Address.IsValid() {
		return fmt.Errorf("store: discovery %s has no address", discovery.MAC)
	}
	if discovery.First.IsZero() {
		discovery.First = discovery.Last
	}

	params := storedb.UpsertDiscoveryParams{
		Mac:      client.NormalizeMAC(discovery.MAC),
		Address:  discovery.Address.Unmap().String(),
		Hostname: discovery.Hostname,
		First:    discovery.First.UnixMilli(),
		Last:     discovery.Last.UnixMilli(),
	}
	if err := s.queries.UpsertDiscovery(ctx, params); err != nil {
		return fmt.Errorf("store: record discovery %s: %w", discovery.MAC, err)
	}
	return nil
}

// DeleteDiscovery removes one prompt, which is what answering it does.
func (s *Store) DeleteDiscovery(ctx context.Context, hardware net.HardwareAddr) error {
	if err := s.queries.DeleteDiscovery(ctx, client.NormalizeMAC(hardware)); err != nil {
		return fmt.Errorf("store: delete discovery %s: %w", hardware, err)
	}
	return nil
}
