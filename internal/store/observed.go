package store

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"aegis/internal/store/storedb"
)

// ObservedRead scopes the observed list. Since is the oldest inclusive
// millisecond and Limit caps the rows.
type ObservedRead struct {
	Since int64
	Limit int
}

// ObservedClient is one address the log saw inside the window, with whether a
// configured client already claims it.
type ObservedClient struct {
	Client   string
	Queries  int64
	LastSeen time.Time
	Claimed  bool
}

// ObservedClients aggregates the query log by source address. It is the
// seen-devices feed: traffic alone puts an address on the list, a configured
// client's address claims it off.
func (s *Store) ObservedClients(ctx context.Context, read ObservedRead) ([]ObservedClient, error) {
	if read.Limit <= 0 {
		read.Limit = 50
	}
	rows, err := s.logQuer.ObservedClients(ctx, storedb.ObservedClientsParams{Time: read.Since, Limit: int64(read.Limit)})
	if err != nil {
		return nil, fmt.Errorf("store: observed clients: %w", err)
	}
	observed := make([]ObservedClient, 0, len(rows))
	for _, row := range rows {
		addr, err := netip.ParseAddr(row.Client)
		if err != nil {
			return nil, fmt.Errorf("store: observed client %q: %w", row.Client, err)
		}
		observed = append(observed, ObservedClient{
			Client:   addr.Unmap().String(),
			Queries:  row.Queries,
			LastSeen: time.UnixMilli(row.LastSeen),
			Claimed:  row.Claimed != 0,
		})
	}
	return observed, nil
}
