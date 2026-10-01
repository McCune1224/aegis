package store_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// hourAgo returns a wall-clock hour boundary far enough inside a window that
// seeding rows at it lands in a predictable bucket.
func hourStart(t *testing.T, hoursAgo int) time.Time {
	t.Helper()
	return time.Now().Add(-time.Duration(hoursAgo) * time.Hour).Truncate(time.Hour)
}

func seedStats(t *testing.T, s *store.Store, entries []store.QueryEntry) {
	t.Helper()
	require.NoError(t, s.RecordQueries(t.Context(), entries))
}

func statEntry(t *testing.T, at time.Time, client, name string, verdict filter.Action) store.QueryEntry {
	t.Helper()
	return store.QueryEntry{
		Time:    at,
		Client:  netip.MustParseAddr(client),
		Name:    mustDomain(t, name),
		Type:    "A",
		Verdict: verdict,
	}
}

func TestStatsSummaryAggregatesTheWindow(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	h1 := hourStart(t, 1)
	h2 := hourStart(t, 2)
	seedStats(t, s, []store.QueryEntry{
		// Hour 1: two ads.example.com blocks from tablet, one allow.
		statEntry(t, h1, "10.0.0.2", "ads.example.com", filter.ActionBlock),
		statEntry(t, h1.Add(time.Minute), "10.0.0.2", "ads.example.com", filter.ActionBlock),
		statEntry(t, h1.Add(2*time.Minute), "10.0.0.2", "example.com", filter.ActionAllow),
		// Hour 2: one block from phone, one allow, one rewrite.
		statEntry(t, h2, "10.0.0.3", "tracker.example.net", filter.ActionBlock),
		statEntry(t, h2.Add(time.Minute), "10.0.0.3", "example.org", filter.ActionAllow),
		statEntry(t, h2.Add(2*time.Minute), "10.0.0.3", "old.example.org", filter.ActionRewrite),
		// Outside a 2h window: a block that must not count.
		statEntry(t, hourStart(t, 5), "10.0.0.9", "stale.example", filter.ActionBlock),
	})

	summary, err := s.Stats(ctx, store.StatsRead{Since: h2.UnixMilli(), Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int64(6), summary.Total)
	require.Equal(t, int64(2), summary.Allowed)
	require.Equal(t, int64(3), summary.Blocked)
	require.Equal(t, int64(1), summary.Rewritten)

	// Two hour buckets, oldest first.
	require.Len(t, summary.Series, 2)
	require.Equal(t, h2.UnixMilli(), summary.Series[0].Start.UnixMilli())
	require.Equal(t, int64(3), summary.Series[0].Total)
	require.Equal(t, int64(1), summary.Series[0].Blocked)
	require.Equal(t, int64(1), summary.Series[0].Rewritten)
	require.Equal(t, h1.UnixMilli(), summary.Series[1].Start.UnixMilli())
	require.Equal(t, int64(3), summary.Series[1].Total)
	require.Equal(t, int64(2), summary.Series[1].Blocked)

	require.Equal(t, []store.NameCount{
		{Name: "ads.example.com", Count: 2},
		{Name: "example.com", Count: 1},
		{Name: "example.org", Count: 1},
		{Name: "old.example.org", Count: 1},
		{Name: "tracker.example.net", Count: 1},
	}, summary.TopQueried, "ties break on the name ascending")
	require.Equal(t, []store.NameCount{
		{Name: "ads.example.com", Count: 2},
		{Name: "tracker.example.net", Count: 1},
	}, summary.TopBlocked)
	require.Equal(t, []store.ClientCount{
		{Client: "10.0.0.2", Count: 3},
		{Client: "10.0.0.3", Count: 3},
	}, summary.TopClients, "equal counts break on the client ascending")
}

func TestStatsIgnoresTheConfiguredHostsAndTheirSubdomains(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	at := hourStart(t, 1)
	seedStats(t, s, []store.QueryEntry{
		statEntry(t, at, "10.0.0.2", "noisy.example", filter.ActionBlock),
		statEntry(t, at, "10.0.0.2", "sub.noisy.example", filter.ActionBlock),
		statEntry(t, at, "10.0.0.2", "quiet.example", filter.ActionAllow),
	})

	require.NoError(t, s.SetStatsIgnored(ctx, []string{"Noisy.Example"}))

	summary, err := s.Stats(ctx, store.StatsRead{Since: at.UnixMilli(), Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.Total, "the ignored host and its subdomain stay out")
	require.Equal(t, []store.NameCount{{Name: "quiet.example", Count: 1}}, summary.TopQueried)

	ignored, err := s.StatsIgnored(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"noisy.example"}, ignored, "the store keeps the lowercase form")
}

func TestStatsResetDeletesOnlyTheWindow(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	inside := hourStart(t, 1)
	outside := hourStart(t, 40)
	seedStats(t, s, []store.QueryEntry{
		statEntry(t, inside, "10.0.0.2", "a.example", filter.ActionAllow),
		statEntry(t, outside, "10.0.0.2", "b.example", filter.ActionAllow),
	})

	require.NoError(t, s.ResetStats(ctx, inside.UnixMilli()))

	rows, err := s.Queries(ctx, store.QueryFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "b.example", rows[0].Name.String())
}

func TestStatsConfigRoundsTripsThroughTheSettings(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	config, err := s.StatsConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, config.Interval, "a fresh store answers with the default")

	config.Interval = 7 * 24 * time.Hour
	require.NoError(t, s.SaveStatsConfig(ctx, config))

	reread, err := s.StatsConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, config, reread)
}
