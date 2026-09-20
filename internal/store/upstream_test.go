package store_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/store"
)

func TestUpstreamsRoundTripThroughTheStore(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "quad9", URL: "9.9.9.9:53", Enabled: true}))
	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "backup", URL: "tls://8.8.8.8", Enabled: false, Backup: true}))

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Upstreams, 2)

	rows, err := s.Upstreams(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Upstream{
		{Name: "backup", URL: "tls://8.8.8.8:853", Enabled: false, Backup: true},
		{Name: "quad9", URL: "udp://9.9.9.9:53", Enabled: true, Backup: false},
	}, rows, "the store keeps the canonical URL and the name as written")

	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "quad9", URL: "tcp://9.9.9.9", Enabled: false}))
	rows, err = s.Upstreams(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2, "an upsert replaces the row, it does not add one")
	require.Equal(t, store.Upstream{Name: "quad9", URL: "tcp://9.9.9.9:53", Enabled: false, Backup: false}, rows[1])

	require.NoError(t, s.DeleteUpstream(ctx, "quad9"))
	rows, err = s.Upstreams(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Upstream{{Name: "backup", URL: "tls://8.8.8.8:853", Enabled: false, Backup: true}}, rows)
}

func TestSaveUpstreamRefusesAURLNoTransportSpeaks(t *testing.T) {
	s := open(t)

	err := s.SaveUpstream(t.Context(), store.Upstream{Name: "bad", URL: "ftp://9.9.9.9", Enabled: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "ftp")
}
