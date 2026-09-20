package main

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/store"
)

func TestSeedUpstreamsFillsAnEmptyTableFromTheFlags(t *testing.T) {
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	require.NoError(t, seedUpstreams(ctx, database, []string{"9.9.9.9:53", "tls://dns.quad9.net"}, logger))

	rows, err := database.Upstreams(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Upstream{
		{Name: "9.9.9.9:53", URL: "udp://9.9.9.9:53", Enabled: true, Backup: false},
		{Name: "tls://dns.quad9.net", URL: "tls://dns.quad9.net:853", Enabled: true, Backup: false},
	}, rows, "the name is the flag as written, the URL is the canonical form")

	require.NoError(t, database.DeleteUpstream(ctx, "9.9.9.9:53"))
	require.NoError(t, seedUpstreams(ctx, database, []string{"9.9.9.9:53"}, logger))

	rows, err = database.Upstreams(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "a table that has rows is never reseeded, first boot or not")
}
