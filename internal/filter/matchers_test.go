package filter_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
)

// ruleSpec builds one rule from a kind and its value, so a test states what the
// rule matches on and not how the spec carries it.
func ruleSpec(id string, kind filter.MatchKind, action filter.Action, value string) filter.RuleSpec {
	spec := filter.RuleSpec{
		ID:     id,
		Source: filter.Source{ID: "local", Name: "Local"},
		Kind:   kind,
		Action: action,
	}
	switch kind {
	case filter.MatchExact, filter.MatchSubdomains:
		spec.Domain = mustParse(value)
	case filter.MatchWildcard, filter.MatchRegex:
		spec.Pattern = value
	case filter.MatchCIDR:
		spec.Network = mustNetwork(value)
	}
	return spec
}

func mustNetwork(raw string) netip.Prefix {
	network, err := filter.ParseNetwork(raw)
	if err != nil {
		panic(err)
	}
	return network
}

// noAddress is the zero Addr, which no network rule can claim, so a test about
// name rules does not state an address it does not care about.
var noAddress netip.Addr

func TestWildcardMatchesExactlyOneLabel(t *testing.T) {
	rs := compile(t, ruleSpec("wild-ads", filter.MatchWildcard, filter.ActionBlock, "*.ads.example"))

	blocked := rs.Decide(domain(t, "srv.ads.example"), "", netip.MustParseAddr("192.168.1.50"), testNow)
	require.Equal(t, filter.ActionBlock, blocked.Action)
	require.NotNil(t, blocked.Match)
	require.Equal(t, "wild-ads", blocked.Match.RuleID)
	require.Equal(t, "*.ads.example", blocked.Match.Pattern)

	for _, miss := range []string{"ads.example", "a.b.ads.example", "example.com"} {
		got := rs.Decide(domain(t, miss), "", noAddress, testNow)
		require.Equal(t, filter.ActionAllow, got.Action, "name=%q", miss)
		require.Nil(t, got.Match, "name=%q", miss)
	}
}

func TestWildcardStarMaySitInTheMiddle(t *testing.T) {
	rs := compile(t, ruleSpec("wild-mid", filter.MatchWildcard, filter.ActionBlock, "ads.*.example"))

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "ads.cdn.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "ads.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "ads.cdn.host.example"), "", noAddress, testNow).Action)
}

func TestWildcardRulesMatchOnTheirTrailingLabels(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    filter.Action
	}{
		{"*.b.c", "x.b.c", filter.ActionBlock},
		{"*.b.c", "b.c", filter.ActionAllow},
		{"*.b.c", "x.y.b.c", filter.ActionAllow},
		{"*.b.c", "x.b.c.d", filter.ActionAllow},
		{"a.*.d.e", "a.x.d.e", filter.ActionBlock},
		{"a.*.d.e", "d.e", filter.ActionAllow},
		{"a.*.d.e", "x.a.x.d.e", filter.ActionAllow},
		{"open.*", "open.x", filter.ActionBlock},
		{"open.*", "open.x.y", filter.ActionAllow},
		{"open.*", "x.open", filter.ActionAllow},
		{"literal.example", "literal.example", filter.ActionBlock},
		{"literal.example", "x.literal.example", filter.ActionAllow},
		{"*.*.tail.example", "a.b.tail.example", filter.ActionBlock},
		{"*.*.tail.example", "b.tail.example", filter.ActionAllow},
		{"tail.example", "tail.example", filter.ActionBlock},
	}
	for _, c := range cases {
		t.Run(c.pattern+" "+c.name, func(t *testing.T) {
			rs := compile(t, ruleSpec("wild", filter.MatchWildcard, filter.ActionBlock, c.pattern))

			got := rs.Decide(domain(t, c.name), "", noAddress, testNow)

			require.Equal(t, c.want, got.Action)
		})
	}
}

// TestEveryWildcardFiresForANameItCanMatch builds a name from each pattern by
// giving every * a label. A rule the index skips for a name it can match is the
// failure this guards, and it is the one a tail bucket can introduce.
func TestEveryWildcardFiresForANameItCanMatch(t *testing.T) {
	patterns := []string{
		"*",
		"*.b.c",
		"a.*.c",
		"a.*.*.b",
		"*.b.c.d.e",
		"a.*.d.e",
		"open.*",
		"literal.example",
	}
	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			rs := compile(t, ruleSpec("wild", filter.MatchWildcard, filter.ActionBlock, pattern))
			matched := strings.ReplaceAll(pattern, "*", "x")

			got := rs.Decide(domain(t, matched), "", noAddress, testNow)

			require.Equal(t, filter.ActionBlock, got.Action, "pattern=%q name=%q", pattern, matched)
			require.NotNil(t, got.Match)
			require.Equal(t, pattern, got.Match.Pattern)
		})
	}
}

// TestRegexRulesAreNotSkippedByALiteralTail keeps a regex on the path that every
// name tests, since its match is unanchored and no literal tail can narrow it.
func TestRegexRulesAreNotSkippedByALiteralTail(t *testing.T) {
	rs := compile(t,
		ruleSpec("wild", filter.MatchWildcard, filter.ActionBlock, "*.track.example"),
		ruleSpec("re", filter.MatchRegex, filter.ActionBlock, `^beacon-[0-9]+$`),
	)

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "x.track.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "beacon-12"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "x.example"), "", noAddress, testNow).Action)
}

func TestWildcardAllowBeatsBlockList(t *testing.T) {
	rs := compile(t,
		hagezi("block-mirror", filter.MatchSubdomains, "mirror.example"),
		ruleSpec("wild-allow", filter.MatchWildcard, filter.ActionAllow, "*.mirror.example"),
	)

	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "eu.mirror.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "mirror.example"), "", noAddress, testNow).Action)
}

func TestRegexMatchesUnanchoredAndCaseInsensitive(t *testing.T) {
	rs := compile(t, ruleSpec("re-track", filter.MatchRegex, filter.ActionBlock, "tracking"))

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "tracking.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "news.tracking.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "example.com"), "", noAddress, testNow).Action)
}

func TestRegexAnchorsAreTheAuthorsJob(t *testing.T) {
	rs := compile(t, ruleSpec("re-cache", filter.MatchRegex, filter.ActionBlock, `^cache-[0-9]+\.cdn\.example$`))

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "cache-12.cdn.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "xcache-1.cdn.example"), "", noAddress, testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "cache-1.cdn.example.com"), "", noAddress, testNow).Action)
}

func TestCIDRRuleMatchesTheClientNetwork(t *testing.T) {
	rs := compile(t, ruleSpec("net-guests", filter.MatchCIDR, filter.ActionBlock, "192.168.8.0/22"))

	blocked := rs.Decide(domain(t, "example.com"), "", netip.MustParseAddr("192.168.9.40"), testNow)
	require.Equal(t, filter.ActionBlock, blocked.Action)
	require.NotNil(t, blocked.Match)
	require.Equal(t, "net-guests", blocked.Match.RuleID)
	require.Equal(t, "192.168.8.0/22", blocked.Match.Pattern)

	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "example.com"), "", netip.MustParseAddr("192.168.12.1"), testNow).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "example.com"), "", netip.MustParseAddr("::ffff:192.168.9.40"), testNow).Action)
}

func TestCIDRRuleMatchesIPv6Networks(t *testing.T) {
	rs := compile(t, ruleSpec("net-v6", filter.MatchCIDR, filter.ActionBlock, "2001:db8::/32"))

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "example.com"), "", netip.MustParseAddr("2001:db8::1"), testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "example.com"), "", netip.MustParseAddr("2001:db9::1"), testNow).Action)
}

func TestNetworkRuleBeatsNameRules(t *testing.T) {
	rs := compile(t,
		hagezi("block-ads", filter.MatchSubdomains, "ads.example"),
		ruleSpec("net-exempt", filter.MatchCIDR, filter.ActionAllow, "10.0.0.0/8"),
	)

	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "ads.example"), "", netip.MustParseAddr("10.1.2.3"), testNow).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "ads.example"), "", netip.MustParseAddr("192.168.1.1"), testNow).Action)
}

func TestNetworkBlockBeatsADomainAllow(t *testing.T) {
	rs := compile(t,
		ruleSpec("allow-docs", filter.MatchExact, filter.ActionAllow, "docs.example"),
		ruleSpec("net-lock", filter.MatchCIDR, filter.ActionBlock, "10.9.0.0/16"),
	)

	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "docs.example"), "", netip.MustParseAddr("10.9.1.2"), testNow).Action)
	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "docs.example"), "", netip.MustParseAddr("10.8.0.1"), testNow).Action)
}

func TestLongestNetworkPrefixWins(t *testing.T) {
	rs := compile(t,
		ruleSpec("net-wide", filter.MatchCIDR, filter.ActionBlock, "10.0.0.0/8"),
		ruleSpec("net-narrow", filter.MatchCIDR, filter.ActionAllow, "10.9.0.0/16"),
	)

	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "example.com"), "", netip.MustParseAddr("10.9.1.2"), testNow).Action)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "example.com"), "", netip.MustParseAddr("10.8.0.1"), testNow).Action)
}

func TestCompileRejectsRulesWithTheWrongPayload(t *testing.T) {
	cases := []struct {
		name string
		spec filter.RuleSpec
	}{
		{
			name: "exact without a domain",
			spec: filter.RuleSpec{ID: "r1", Kind: filter.MatchExact, Action: filter.ActionBlock},
		},
		{
			name: "wildcard with a domain and no pattern",
			spec: filter.RuleSpec{ID: "r2", Kind: filter.MatchWildcard, Domain: mustParse("a.example"), Action: filter.ActionBlock},
		},
		{
			name: "cidr with a domain and no network",
			spec: filter.RuleSpec{ID: "r3", Kind: filter.MatchCIDR, Domain: mustParse("a.example"), Action: filter.ActionBlock},
		},
		{
			name: "subdomains with a pattern",
			spec: filter.RuleSpec{ID: "r4", Kind: filter.MatchSubdomains, Pattern: "a.example", Action: filter.ActionBlock},
		},
		{
			name: "regex that does not compile",
			spec: ruleSpec("r5", filter.MatchRegex, filter.ActionBlock, "([)+"),
		},
		{
			name: "wildcard with a malformed label",
			spec: ruleSpec("r6", filter.MatchWildcard, filter.ActionBlock, "a**b.example"),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := filter.Compile(defaultConfig([]filter.RuleSpec{testCase.spec}))
			require.Error(t, err)
		})
	}
}

func TestParsePatternCanonicalizes(t *testing.T) {
	pattern, err := filter.ParsePattern(filter.MatchWildcard, " *.ADS.Example ")
	require.NoError(t, err)
	require.Equal(t, "*.ads.example", pattern)

	pattern, err = filter.ParsePattern(filter.MatchRegex, "Tracking")
	require.NoError(t, err)
	require.Equal(t, "Tracking", pattern)

	_, err = filter.ParsePattern(filter.MatchRegex, "([)+")
	require.Error(t, err)

	_, err = filter.ParsePattern(filter.MatchWildcard, "bad..label")
	require.Error(t, err)
}

func TestParseNetworkMasksThePrefix(t *testing.T) {
	network, err := filter.ParseNetwork("192.168.1.5/24")
	require.NoError(t, err)
	require.Equal(t, "192.168.1.0/24", network.String())

	_, err = filter.ParseNetwork("not-a-network")
	require.Error(t, err)
}
