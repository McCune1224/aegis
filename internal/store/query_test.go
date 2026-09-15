package store_test

import (
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
)

func TestRecordedQueriesComeBackThroughTheFilters(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	base := time.Now().Truncate(time.Millisecond)
	batch := []store.QueryEntry{
		{
			Time:    base,
			Client:  netip.MustParseAddr("10.9.9.2"),
			Name:    mustDomain(t, "ads.example.com"),
			Type:    "A",
			Verdict: filter.ActionBlock,
			Rule:    "list.txt:1",
		},
		{
			Time:    base.Add(time.Second),
			Client:  netip.MustParseAddr("10.9.9.3"),
			Name:    mustDomain(t, "example.com"),
			Type:    "AAAA",
			Verdict: filter.ActionAllow,
		},
	}
	require.NoError(t, s.RecordQueries(ctx, batch))

	got, err := s.Queries(ctx, store.QueryFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, batch[1].Name, got[0].Name, "newest first")
	require.Equal(t, base, got[1].Time)

	blocked := filter.ActionBlock
	blockedQueries, err := s.Queries(ctx, store.QueryFilter{Verdict: &blocked, Limit: 10})
	require.NoError(t, err)
	require.Len(t, blockedQueries, 1)
	require.Equal(t, "ads.example.com", blockedQueries[0].Name.String())

	byClient, err := s.Queries(ctx, store.QueryFilter{Client: netip.MustParseAddr("10.9.9.3").String(), Limit: 10})
	require.NoError(t, err)
	require.Len(t, byClient, 1)
	require.Equal(t, "example.com", byClient[0].Name.String())

	byName, err := s.Queries(ctx, store.QueryFilter{Name: "example.com", Limit: 10})
	require.NoError(t, err)
	require.Len(t, byName, 2, "the name filter covers the name and its subdomains")

	limitTwo, err := s.Queries(ctx, store.QueryFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, limitTwo, 2)
	limitOne, err := s.Queries(ctx, store.QueryFilter{Limit: 1})
	require.NoError(t, err)
	require.Len(t, limitOne, 1)
}

func TestQueriesTrimToTheRetentionBound(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	base := time.Now().Truncate(time.Millisecond)
	batch := make([]store.QueryEntry, 0, 5)
	for i := range 5 {
		batch = append(batch, store.QueryEntry{
			Time:    base.Add(time.Duration(i) * time.Second),
			Client:  netip.MustParseAddr("10.9.9.2"),
			Name:    mustDomain(t, "ads.example.com"),
			Type:    "A",
			Verdict: filter.ActionBlock,
		})
	}
	require.NoError(t, s.RecordQueries(ctx, batch))
	require.NoError(t, s.TrimQueries(ctx, 3))

	got, err := s.Queries(ctx, store.QueryFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, base.Add(4*time.Second), got[0].Time, "the newest rows survive")
}

func TestReadsRunBesideTheLogWriter(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	const batches = 100
	const perBatch = 20
	written := make(chan struct{})
	go func() {
		defer close(written)
		for i := range batches {
			batch := make([]store.QueryEntry, 0, perBatch)
			for j := range perBatch {
				batch = append(batch, store.QueryEntry{
					Time:    time.Now(),
					Client:  netip.MustParseAddr("10.9.9.2"),
					Name:    mustDomain(t, "ads.example.com"),
					Type:    "A",
					Verdict: filter.ActionBlock,
					Rule:    strconv.Itoa(i*perBatch + j),
				})
			}
			if err := s.RecordQueries(ctx, batch); err != nil {
				t.Errorf("record batch %d: %v", i, err)
				return
			}
		}
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		entries, err := s.Queries(ctx, store.QueryFilter{Limit: 10})
		require.NoError(t, err)
		require.LessOrEqual(t, len(entries), 10)
		select {
		case <-written:
			return
		case <-time.After(10 * time.Millisecond):
		}
		require.Less(t, time.Now().UnixNano(), deadline.UnixNano(), "the writer never finished")
	}
}
