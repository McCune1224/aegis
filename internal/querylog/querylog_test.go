package querylog_test

import (
	"io"
	"log/slog"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	mdns "github.com/miekg/dns"

	"aegis/internal/dns"
	"aegis/internal/filter"
	"aegis/internal/querylog"
	"aegis/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func decisionFor(name string, action filter.Action) dns.Decision {
	domain, err := filter.ParseDomain(name)
	if err != nil {
		panic(err)
	}
	return dns.Decision{
		Time:    time.Now(),
		Address: netip.MustParseAddr("10.9.9.2"),
		Name:    domain,
		Type:    mdns.TypeToString[mdns.TypeA],
		Action:  action,
	}
}

func TestObservedDecisionsReachTheLog(t *testing.T) {
	database := open(t)
	log := querylog.New(database, quiet())
	defer func() { _ = log.Close() }()

	log.Observe(decisionFor("ads.example.com", filter.ActionBlock))
	log.Observe(decisionFor("example.com", filter.ActionAllow))

	entries := waitFor(t, database, 2)
	require.Equal(t, "example.com", entries[0].Name.String(), "newest first")
	require.Equal(t, filter.ActionAllow, entries[0].Verdict)
	require.Equal(t, "ads.example.com", entries[1].Name.String())
	require.Equal(t, filter.ActionBlock, entries[1].Verdict)
	require.Equal(t, "A", entries[1].Type)
	require.Equal(t, netip.MustParseAddr("10.9.9.2"), entries[1].Client)
}

func TestCloseFlushesWhatItHolds(t *testing.T) {
	database := open(t)
	log := querylog.New(database, quiet())

	log.Observe(decisionFor("ads.example.com", filter.ActionBlock))
	require.NoError(t, log.Close())

	entries, err := database.Queries(t.Context(), store.QueryFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestTheLogFlushesAtTheBatchBound(t *testing.T) {
	database := open(t)
	log := querylog.New(database, quiet())
	defer func() { _ = log.Close() }()

	// One decision short of the bound nothing persists: the writer is waiting
	// on its period, which has not elapsed.
	for range querylog.FlushSize - 1 {
		log.Observe(decisionFor("ads.example.com", filter.ActionBlock))
	}
	time.Sleep(50 * time.Millisecond)
	entries, err := database.Queries(t.Context(), store.QueryFilter{Limit: querylog.FlushSize})
	require.NoError(t, err)
	require.Empty(t, entries, "nothing persists before the bound or the period")

	// The decision that reaches the bound flushes the whole batch at once.
	log.Observe(decisionFor("example.com", filter.ActionAllow))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, err = database.Queries(t.Context(), store.QueryFilter{Limit: querylog.FlushSize})
		require.NoError(t, err)
		if len(entries) == querylog.FlushSize {
			require.Equal(t, "example.com", entries[0].Name.String(), "newest first")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the bound never flushed, held %d entries", len(entries))
}

func TestTheLogTrimsItselfToTheBound(t *testing.T) {
	database := open(t)
	log := querylog.New(database, quiet())
	defer func() { _ = log.Close() }()

	for range querylog.MaxRows + 2*querylog.FlushSize {
		log.Observe(decisionFor("ads.example.com", filter.ActionBlock))
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := database.Queries(t.Context(), store.QueryFilter{Limit: querylog.MaxRows + 1})
		require.NoError(t, err)
		if len(entries) <= querylog.MaxRows {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the log never trimmed to its bound")
}

func waitFor(t *testing.T, database *store.Store, want int) []store.QueryEntry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := database.Queries(t.Context(), store.QueryFilter{Limit: 10})
		require.NoError(t, err)
		if len(entries) == want {
			return entries
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the log never held %d entries", want)
	return nil
}

func BenchmarkObserve(b *testing.B) {
	b.StopTimer()
	database, err := store.Open(b.Context(), filepath.Join(b.TempDir(), "aegis.db"))
	require.NoError(b, err)
	b.StartTimer()
	defer func() { _ = database.Close() }()

	log := querylog.New(database, quiet())
	defer func() { _ = log.Close() }()

	decision := decisionFor("ads.example.com", filter.ActionBlock)
	b.ReportAllocs()
	for b.Loop() {
		log.Observe(decision)
	}
	b.StopTimer()
	entries, err := database.Queries(b.Context(), store.QueryFilter{Limit: 1})
	require.NoError(b, err)
	b.Logf("%d decisions observed, %d recorded, %d dropped", b.N, len(entries), log.Dropped())
}

func TestTheLogStampsTheThreatAFeedNames(t *testing.T) {
	database := open(t)

	log := querylog.New(database, quiet(), querylog.WithThreats(func(name string) string {
		if name == "c2.example.biz" {
			return "c2"
		}
		return ""
	}))
	log.Observe(decisionFor("c2.example.biz", filter.ActionAllow))
	log.Observe(decisionFor("clean.example.org", filter.ActionAllow))
	require.NoError(t, log.Close())

	entries, err := database.Queries(t.Context(), store.QueryFilter{Limit: 10})
	require.NoError(t, err)

	byName := make(map[string]string, len(entries))
	for _, entry := range entries {
		byName[entry.Name.String()] = entry.Threat
	}
	require.Equal(t, "c2", byName["c2.example.biz"])
	require.Equal(t, "", byName["clean.example.org"])
}
