package store_test

import (
	"database/sql"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"aegis/internal/store"
)

func TestAccessSetsRoundTripThroughTheStore(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	require.NoError(t, s.SetAccess(ctx,
		[]netip.Prefix{netip.MustParsePrefix("10.9.9.0/24"), netip.MustParsePrefix("192.168.9.2/32")},
		[]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12")},
	))

	allowed, disallowed, err := s.Access(ctx)
	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("10.9.9.0/24"),
		netip.MustParsePrefix("192.168.9.2/32"),
	}, allowed)
	require.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
	}, disallowed)

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, allowed, cfg.Allowed)
	require.Equal(t, disallowed, cfg.Disallowed)

	require.NoError(t, s.SetAccess(ctx, []netip.Prefix{netip.MustParsePrefix("10.9.9.5/24")}, nil))

	cfg, err = s.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, cfg.Allowed,
		"a write with host bits set stores the masked prefix")
	require.Empty(t, cfg.Disallowed, "a replace removes the rows the last write left")
}

func TestValidateRefusesBrokenAccessSets(t *testing.T) {
	s := open(t)

	cases := []struct {
		name       string
		allowed    []netip.Prefix
		disallowed []netip.Prefix
		want       string
	}{
		{
			name:    "host bits set",
			allowed: []netip.Prefix{netip.MustParsePrefix("10.9.9.5/24")},
			want:    "host bits",
		},
		{
			name:       "listed in both sets",
			allowed:    []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")},
			disallowed: []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")},
			want:       "10.9.9.0/24",
		},
		{
			name:    "listed twice in one set",
			allowed: []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24"), netip.MustParsePrefix("10.9.9.0/24")},
			want:    "10.9.9.0/24",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := s.Load(t.Context())
			require.NoError(t, err)
			cfg.Allowed = tc.allowed
			cfg.Disallowed = tc.disallowed

			err = cfg.Validate()

			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidateAcceptsCleanAccessSets(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SetAccess(ctx,
		[]netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")},
		[]netip.Prefix{netip.MustParsePrefix("10.9.9.2/32")},
	))

	cfg, err := s.Load(ctx)

	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}

func TestAccessSetsSurviveAReopen(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "aegis.db")

	first, err := store.Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, first.SetAccess(ctx,
		[]netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")},
		[]netip.Prefix{netip.MustParsePrefix("192.168.9.2/32")},
	))
	require.NoError(t, first.Close())

	second, err := store.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	cfg, err := second.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, cfg.Allowed)
	require.Equal(t, []netip.Prefix{netip.MustParsePrefix("192.168.9.2/32")}, cfg.Disallowed)
}

func TestALoadNamesTheAccessRowThatNoLongerParses(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "aegis.db")
	s, err := store.Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, s.SetAccess(ctx, []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, nil))
	require.NoError(t, s.Close())

	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = raw.ExecContext(ctx, "UPDATE access SET cidr = 'not-a-prefix' WHERE kind = 'allow'")
	require.NoError(t, err)

	reopened, err := store.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })

	_, err = reopened.Load(ctx)

	require.Error(t, err)
	require.Contains(t, err.Error(), "not-a-prefix")
	require.Contains(t, err.Error(), "allow")
}
