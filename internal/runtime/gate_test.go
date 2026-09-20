package runtime_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/runtime"
)

var stranger = netip.MustParseAddr("192.168.9.9")
var sibling = netip.MustParseAddr("10.9.9.3")

func TestGateSemantics(t *testing.T) {
	cases := []struct {
		name       string
		allowed    []netip.Prefix
		disallowed []netip.Prefix
		address    netip.Addr
		want       bool
	}{
		{"empty sets serve everyone", nil, nil, tablet, true},
		{"a listed client is served", []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, nil, tablet, true},
		{"a permit-list refuses a stranger", []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, nil, stranger, false},
		{"disallowed wins over allowed", []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, []netip.Prefix{netip.MustParsePrefix("10.9.9.2/32")}, tablet, false},
		{"a listed sibling of a disallowed address still resolves", []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, []netip.Prefix{netip.MustParsePrefix("10.9.9.2/32")}, sibling, true},
		{"an empty allowed set refuses only the denied", nil, []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, tablet, false},
		{"an empty allowed set serves an unlisted address", nil, []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}, stranger, true},
		{"a mapped IPv4 address matches the IPv4 row", nil, []netip.Prefix{netip.MustParsePrefix("10.9.9.2/32")}, netip.MustParseAddr("::ffff:10.9.9.2"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openStore(t)
			require.NoError(t, s.SetAccess(t.Context(), tc.allowed, tc.disallowed))
			rt := runtime.New(s, nil, quietLogger())
			require.NoError(t, rt.Reload(t.Context()))

			require.Equal(t, tc.want, rt.Allows(tc.address))
		})
	}
}

func TestEveryoneMayAskBeforeTheFirstReload(t *testing.T) {
	rt := runtime.New(openStore(t), nil, quietLogger())

	require.True(t, rt.Allows(tablet))
}

func TestAnAccessEditTakesEffectWithoutARestart(t *testing.T) {
	ctx := t.Context()
	s := openStore(t)
	rt := runtime.New(s, nil, quietLogger())
	require.NoError(t, rt.Reload(ctx))
	require.True(t, rt.Allows(tablet))

	require.NoError(t, s.SetAccess(ctx, nil, []netip.Prefix{netip.MustParsePrefix("10.9.9.2/32")}))
	require.NoError(t, rt.Reload(ctx))
	require.False(t, rt.Allows(tablet), "the deny is live on the same runtime")
	require.True(t, rt.Allows(sibling), "the empty allowed set still serves the unlisted")

	require.NoError(t, s.SetAccess(ctx, nil, nil))
	require.NoError(t, rt.Reload(ctx))
	require.True(t, rt.Allows(tablet), "removing the deny is live too")
}
