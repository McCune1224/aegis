package threat

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/dns"
	"aegis/internal/filter"
	"aegis/internal/store"
)

func TestServiceRecordsFindingsFromADecisionStream(t *testing.T) {
	database, err := store.Open(t.Context(), t.TempDir()+"/aegis.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	base := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	next := base
	service := NewService(database, nil, WithClock(func() time.Time { return next }))

	observe := func(name string) {
		parsed, err := filter.ParseDomain(name)
		require.NoError(t, err)
		service.Observe(dns.Decision{
			Time:    next,
			Address: netip.MustParseAddr("10.9.9.44"),
			Name:    parsed,
			Type:    "A",
			Action:  filter.ActionAllow,
		})
		next = next.Add(time.Second)
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
	next := base
	service := NewService(database, nil, WithClock(func() time.Time { return next }))

	for i := 0; i < 40; i++ {
		parsed, err := filter.ParseDomain("example.com")
		require.NoError(t, err)
		service.Observe(dns.Decision{
			Time:    next,
			Address: netip.MustParseAddr("10.9.9.44"),
			Name:    parsed,
			Type:    "A",
			Action:  filter.ActionAllow,
		})
		next = next.Add(time.Second)
	}

	require.NoError(t, service.Close())

	findings, err := database.ThreatFindings(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, findings)
}
