package store_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/rewrite"
)

func TestRewritesRoundTripThroughTheStore(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	address, err := rewrite.Parse("home.local", "192.168.1.50")
	require.NoError(t, err)
	require.NoError(t, s.SaveRewrite(ctx, address))
	cname, err := rewrite.Parse("*.alias.local", "home.local")
	require.NoError(t, err)
	require.NoError(t, s.SaveRewrite(ctx, cname))

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Rewrites, 2)

	got, err := rewrite.Parse("home.local", "192.168.1.55")
	require.NoError(t, err)
	require.NoError(t, s.SaveRewrite(ctx, got))

	records, err := s.Rewrites(ctx)
	require.NoError(t, err)
	require.Len(t, records, 2)
	byPattern := map[string]rewrite.Record{}
	for _, record := range records {
		byPattern[record.Pattern] = record
	}
	require.Equal(t, "192.168.1.55", byPattern["home.local"].Addr.String())
	require.Equal(t, "home.local", byPattern["*.alias.local"].CName.String())

	require.NoError(t, s.DeleteRewrite(ctx, "*.alias.local"))
	records, err = s.Rewrites(ctx)
	require.NoError(t, err)
	require.Len(t, records, 1)
}
