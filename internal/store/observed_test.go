package store_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
)

func TestObservedClientsAggregateQueryTraffic(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveClient(ctx, store.Client{
		Key:       "tablet",
		Profile:   "default",
		Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.2")},
	}))

	hourAgo := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	twoHoursAgo := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.RecordQueries(ctx, []store.QueryEntry{
		{Time: hourAgo, Client: netip.MustParseAddr("10.0.0.2"), Name: mustDomain(t, "a.example"), Type: "A", Verdict: filter.ActionAllow},
		{Time: hourAgo.Add(time.Minute), Client: netip.MustParseAddr("10.0.0.2"), Name: mustDomain(t, "b.example"), Type: "A", Verdict: filter.ActionAllow},
		{Time: twoHoursAgo, Client: netip.MustParseAddr("10.0.0.3"), Name: mustDomain(t, "c.example"), Type: "A", Verdict: filter.ActionBlock},
		{Time: time.Now().Add(-40 * time.Hour), Client: netip.MustParseAddr("10.9.9.9"), Name: mustDomain(t, "d.example"), Type: "A", Verdict: filter.ActionAllow},
	}))

	observed, err := s.ObservedClients(ctx, store.ObservedRead{Since: time.Now().Add(-24 * time.Hour).UnixMilli(), Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []store.ObservedClient{
		{Client: "10.0.0.2", Queries: 2, LastSeen: hourAgo.Add(time.Minute), Claimed: true},
		{Client: "10.0.0.3", Queries: 1, LastSeen: twoHoursAgo, Claimed: false},
	}, observed, "the claimed device is marked, the stale row stays out, more traffic ranks first")
}
