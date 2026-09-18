package rewrite_test

import (
	"testing"

	"net/netip"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/rewrite"
)

func mustParse(t *testing.T, raw string) filter.Domain {
	t.Helper()
	name, err := filter.ParseDomain(raw)
	require.NoError(t, err)
	return name
}

func mustRecord(t *testing.T, pattern, target string) rewrite.Record {
	t.Helper()
	record, err := rewrite.Parse(pattern, target)
	require.NoError(t, err)
	return record
}

func newTable(t *testing.T, entries ...[2]string) *rewrite.Table {
	t.Helper()
	records := make([]rewrite.Record, 0, len(entries))
	for _, entry := range entries {
		records = append(records, mustRecord(t, entry[0], entry[1]))
	}
	return rewrite.New(records)
}

func TestParseReadsExactAndWildcardPatterns(t *testing.T) {
	address := mustRecord(t, "home.local", "192.168.1.50")
	require.Equal(t, "home.local", address.Pattern)
	require.False(t, address.Wildcard)
	require.Equal(t, netip.MustParseAddr("192.168.1.50"), address.Addr)
	require.Equal(t, "", address.CName.String())

	wildcard := mustRecord(t, "*.home.local", "192.168.1.51")
	require.Equal(t, "*.home.local", wildcard.Pattern)
	require.True(t, wildcard.Wildcard)
	require.Equal(t, netip.MustParseAddr("192.168.1.51"), wildcard.Addr)

	cname := mustRecord(t, "alias.home.local", "home.local")
	require.Equal(t, "home.local", cname.CName.String())
	require.False(t, cname.Addr.IsValid())
}

func TestParseRejectsUnusablePairs(t *testing.T) {
	for _, tc := range [][2]string{
		{"", "192.168.1.50"},
		{"home.local", ""},
		{"home.local", "not an address or a name"},
		{"*.home.local", "not a name either"},
		{"home..local", "192.168.1.50"},
		{"*.example.com:84", "192.168.1.50"},
	} {
		t.Run(tc[0]+"="+tc[1], func(t *testing.T) {
			_, err := rewrite.Parse(tc[0], tc[1])
			require.Error(t, err)
		})
	}
}

func TestTableAnswersExactNames(t *testing.T) {
	table := newTable(t, [2]string{"home.local", "192.168.1.50"})

	got, ok := table.Lookup(mustParse(t, "home.local"))

	require.True(t, ok)
	require.Equal(t, netip.MustParseAddr("192.168.1.50"), got.Addr)
}

func TestTableMatchesWildcardSubdomainsAtAnyDepthButNotTheApex(t *testing.T) {
	table := newTable(t, [2]string{"*.example.com", "192.168.1.60"})

	for _, name := range []string{"a.example.com", "a.b.example.com", "a.b.c.example.com"} {
		got, ok := table.Lookup(mustParse(t, name))
		require.True(t, ok, name)
		require.Equal(t, netip.MustParseAddr("192.168.1.60"), got.Addr, name)
	}

	_, ok := table.Lookup(mustParse(t, "example.com"))
	require.False(t, ok, "the apex is not matched by its wildcard")
}

func TestTablePrefersTheLongestMatchingPattern(t *testing.T) {
	table := newTable(t,
		[2]string{"*.example.com", "192.168.1.60"},
		[2]string{"sub.example.com", "192.168.1.70"},
	)

	got, ok := table.Lookup(mustParse(t, "sub.example.com"))
	require.True(t, ok)
	require.Equal(t, netip.MustParseAddr("192.168.1.70"), got.Addr)

	got, ok = table.Lookup(mustParse(t, "other.example.com"))
	require.True(t, ok)
	require.Equal(t, netip.MustParseAddr("192.168.1.60"), got.Addr)
}

func TestReverseAnswersLookupsFromExactAddressRewrites(t *testing.T) {
	table := newTable(t,
		[2]string{"home.local", "192.168.1.50"},
		[2]string{"*.example.com", "192.168.1.60"},
	)

	name, ok := table.Reverse(netip.MustParseAddr("192.168.1.50"))
	require.True(t, ok)
	require.Equal(t, "home.local", name.String())

	_, ok = table.Reverse(netip.MustParseAddr("192.168.1.60"))
	require.False(t, ok, "a wildcard address is not one host and earns no PTR")
}

func TestReverseNamesRoundTripThroughArpaForm(t *testing.T) {
	require.Equal(t, "50.1.168.192.in-addr.arpa", rewrite.ReverseName(netip.MustParseAddr("192.168.1.50")))

	address, ok := rewrite.ParseReverse(mustParse(t, "50.1.168.192.in-addr.arpa"))
	require.True(t, ok)
	require.Equal(t, netip.MustParseAddr("192.168.1.50"), address)

	sixteen, ok := rewrite.ParseReverse(mustParse(t, rewrite.ReverseName(netip.MustParseAddr("2001:db8::1"))))
	require.True(t, ok)
	require.Equal(t, netip.MustParseAddr("2001:db8::1"), sixteen)

	_, ok = rewrite.ParseReverse(mustParse(t, "example.com"))
	require.False(t, ok)
}
