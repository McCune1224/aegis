package threat

import (
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/dns"
	"aegis/internal/filter"
	"aegis/internal/store"
)

// syncedClock is safe to read from the analyser goroutine while the test
// advances it, which the race detector rightly refuses over a bare variable.
type syncedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *syncedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *syncedClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestServiceRecordsFindingsFromADecisionStream(t *testing.T) {
	database, err := store.Open(t.Context(), t.TempDir()+"/aegis.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	base := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	clock := &syncedClock{now: base}
	service := NewService(database, nil, WithClock(clock.Now))

	observe := func(name string) {
		parsed, err := filter.ParseDomain(name)
		require.NoError(t, err)
		service.Observe(dns.Decision{
			Time:    clock.Now(),
			Address: netip.MustParseAddr("10.9.9.44"),
			Name:    parsed,
			Type:    "A",
			Action:  filter.ActionAllow,
		})
		clock.advance(time.Second)
	}
	for i := 0; i <= dgaThreshold; i++ {
		observe(dgaName(i))
	}

	require.NoError(t, service.Close())

	findings, err := database.ThreatFindings(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, "dga", findings[0].Kind)
	require.Equal(t, "10.9.9.44", findings[0].Client)
	require.NotEmpty(t, findings[0].Evidence)
}

func TestServiceKeepsAQuietNetworkSilent(t *testing.T) {
	database, err := store.Open(t.Context(), t.TempDir()+"/aegis.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	base := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	clock := &syncedClock{now: base}
	service := NewService(database, nil, WithClock(clock.Now))

	for i := 0; i < 40; i++ {
		parsed, err := filter.ParseDomain("example.com")
		require.NoError(t, err)
		service.Observe(dns.Decision{
			Time:    clock.Now(),
			Address: netip.MustParseAddr("10.9.9.44"),
			Name:    parsed,
			Type:    "A",
			Action:  filter.ActionAllow,
		})
		clock.advance(time.Second)
	}

	require.NoError(t, service.Close())

	findings, err := database.ThreatFindings(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, findings)
}
