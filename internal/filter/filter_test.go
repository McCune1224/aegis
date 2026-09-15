package filter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
)

func domain(t *testing.T, raw string) filter.Domain {
	t.Helper()
	d, err := filter.ParseDomain(raw)
	require.NoError(t, err)
	return d
}

func compile(t *testing.T, specs ...filter.RuleSpec) *filter.RuleSet {
	t.Helper()
	rs, err := filter.Compile(defaultConfig(specs))
	require.NoError(t, err)
	return rs
}

// defaultConfig wraps rules in the smallest config Compile accepts, so a test
// about rules does not restate the profile it does not care about.
func defaultConfig(specs []filter.RuleSpec) filter.Config {
	return filter.Config{
		Rules:    specs,
		Profiles: []filter.ProfileSpec{{ID: "default"}},
		Default:  "default",
	}
}

func hagezi(id string, kind filter.MatchKind, name string) filter.RuleSpec {
	return filter.RuleSpec{
		ID:     id,
		Source: filter.Source{ID: "hagezi", Name: "Hagezi"},
		Kind:   kind,
		Domain: mustParse(name),
		Action: filter.ActionBlock,
	}
}

func mustParse(name string) filter.Domain {
	d, err := filter.ParseDomain(name)
	if err != nil {
		panic(err)
	}
	return d
}

func TestParseDomainNormalizes(t *testing.T) {
	cases := map[string]string{
		"Example.COM.":        "example.com",
		"  ads.example.com  ": "ads.example.com",
		"localhost":           "localhost",
		"a.b.c.":              "a.b.c",
	}
	for raw, want := range cases {
		got, err := filter.ParseDomain(raw)
		require.NoError(t, err, "raw=%q", raw)
		require.Equal(t, want, got.String(), "raw=%q", raw)
	}
}

func TestParseDomainRejectsEmpty(t *testing.T) {
	for _, raw := range []string{"", ".", "   ", "..", "example..com"} {
		got, err := filter.ParseDomain(raw)
		require.Error(t, err, "raw=%q", raw)
		require.Equal(t, "", got.String(), "raw=%q", raw)
	}
}

func TestDecideAppliesAllowBeforeBlock(t *testing.T) {
	rs := compile(t,
		hagezi("block-ads", filter.MatchSubdomains, "doubleclick.net"),
		filter.RuleSpec{
			ID:     "allow-news",
			Source: filter.Source{ID: "local", Name: "Local"},
			Kind:   filter.MatchExact,
			Domain: mustParse("news.doubleclick.net"),
			Action: filter.ActionAllow,
		},
	)

	got := rs.Decide(domain(t, "news.doubleclick.net"), "")

	require.Equal(t, filter.ActionAllow, got.Action)
	require.NotNil(t, got.Match)
	require.Equal(t, "allow-news", got.Match.RuleID)
	require.Equal(t, "Local", got.Match.Source.Name)
}

func TestDecideSubdomainRuleMatchesTheDomainItself(t *testing.T) {
	rs := compile(t, hagezi("block-ads", filter.MatchSubdomains, "doubleclick.net"))

	got := rs.Decide(domain(t, "doubleclick.net"), "")

	require.Equal(t, filter.ActionBlock, got.Action)
	require.NotNil(t, got.Match)
	require.Equal(t, "block-ads", got.Match.RuleID)
}

func TestDecideExactRuleDoesNotMatchChildName(t *testing.T) {
	rs := compile(t, hagezi("block-exact", filter.MatchExact, "ads.example.com"))

	require.Equal(t, filter.ActionAllow, rs.Decide(domain(t, "cdn.ads.example.com"), "").Action)
	require.Nil(t, rs.Decide(domain(t, "cdn.ads.example.com"), "").Match)
	require.Equal(t, filter.ActionBlock, rs.Decide(domain(t, "ads.example.com"), "").Action)
}

func TestDecideWithoutAMatchAllows(t *testing.T) {
	rs := compile(t)

	got := rs.Decide(domain(t, "example.com"), "")

	require.Equal(t, filter.ActionAllow, got.Action)
	require.Nil(t, got.Match)
}

func TestDecidePrefersTheMoreSpecificRule(t *testing.T) {
	rs := compile(t,
		hagezi("block-net", filter.MatchSubdomains, "doubleclick.net"),
		filter.RuleSpec{
			ID:     "block-ads",
			Source: filter.Source{ID: "oisd", Name: "OISD"},
			Kind:   filter.MatchSubdomains,
			Domain: mustParse("ads.doubleclick.net"),
			Action: filter.ActionBlock,
		},
	)

	got := rs.Decide(domain(t, "x.ads.doubleclick.net"), "")

	require.Equal(t, filter.ActionBlock, got.Action)
	require.NotNil(t, got.Match)
	require.Equal(t, "block-ads", got.Match.RuleID)
}

func TestDecidePrefersExactOverSubdomain(t *testing.T) {
	rs := compile(t,
		hagezi("block-sub", filter.MatchSubdomains, "ads.example.com"),
		filter.RuleSpec{
			ID:     "block-exact",
			Source: filter.Source{ID: "oisd", Name: "OISD"},
			Kind:   filter.MatchExact,
			Domain: mustParse("ads.example.com"),
			Action: filter.ActionBlock,
		},
	)

	got := rs.Decide(domain(t, "ads.example.com"), "")

	require.NotNil(t, got.Match)
	require.Equal(t, "block-exact", got.Match.RuleID)
}

func TestDecideBreaksTiesByDeclarationOrder(t *testing.T) {
	rs := compile(t,
		hagezi("first", filter.MatchSubdomains, "example.com"),
		filter.RuleSpec{
			ID:     "second",
			Source: filter.Source{ID: "oisd", Name: "OISD"},
			Kind:   filter.MatchSubdomains,
			Domain: mustParse("example.com"),
			Action: filter.ActionBlock,
		},
	)

	got := rs.Decide(domain(t, "example.com"), "")

	require.NotNil(t, got.Match)
	require.Equal(t, "first", got.Match.RuleID)
}

func TestDecideKeepsAllowFromOneSourceWhenAnotherBlocks(t *testing.T) {
	rs := compile(t,
		hagezi("block-a", filter.MatchSubdomains, "example.com"),
		filter.RuleSpec{
			ID:     "allow-a",
			Source: filter.Source{ID: "local", Name: "Local"},
			Kind:   filter.MatchSubdomains,
			Domain: mustParse("example.com"),
			Action: filter.ActionAllow,
		},
		filter.RuleSpec{
			ID:     "block-b",
			Source: filter.Source{ID: "oisd", Name: "OISD"},
			Kind:   filter.MatchSubdomains,
			Domain: mustParse("www.example.com"),
			Action: filter.ActionBlock,
		},
	)

	got := rs.Decide(domain(t, "www.example.com"), "")

	require.Equal(t, filter.ActionAllow, got.Action)
	require.NotNil(t, got.Match)
	require.Equal(t, "allow-a", got.Match.RuleID)
}

func TestCompileRejectsBadRules(t *testing.T) {
	_, err := filter.Compile(defaultConfig([]filter.RuleSpec{{
		ID:     "bad-kind",
		Source: filter.Source{ID: "local", Name: "Local"},
		Kind:   filter.MatchKind(99),
		Domain: mustParse("example.com"),
		Action: filter.ActionBlock,
	}}))
	require.Error(t, err)

	_, err = filter.Compile(defaultConfig([]filter.RuleSpec{{
		ID:     "no-domain",
		Source: filter.Source{ID: "local", Name: "Local"},
		Kind:   filter.MatchExact,
		Action: filter.ActionBlock,
	}}))
	require.Error(t, err)
}

func TestParseDomainRejectsCharactersThatNeverAppearInAName(t *testing.T) {
	for _, raw := range []string{
		"exa mple.com",
		"example.com/path",
		"*.example.com",
		"example,com",
		"ex\u00e4mple.com",
		"http://example.com",
	} {
		got, err := filter.ParseDomain(raw)
		require.Error(t, err, "raw=%q", raw)
		require.Equal(t, "", got.String(), "raw=%q", raw)
	}
}

func TestParseDomainAcceptsTheUnderscoreLabelsDnsActuallyUses(t *testing.T) {
	got, err := filter.ParseDomain("_dmarc.example.com")

	require.NoError(t, err)
	require.Equal(t, "_dmarc.example.com", got.String())
}

func TestActionAndMatchKindRoundTripWithStrings(t *testing.T) {
	for _, name := range []string{"block", "allow"} {
		action, err := filter.ParseAction(name)
		require.NoError(t, err, "name=%q", name)
		require.Equal(t, name, action.String(), "name=%q", name)
	}
	for _, name := range []string{"exact", "subdomains"} {
		kind, err := filter.ParseMatchKind(name)
		require.NoError(t, err, "name=%q", name)
		require.Equal(t, name, kind.String(), "name=%q", name)
	}
}

func TestParseActionAndMatchKindRejectAnUnknownName(t *testing.T) {
	_, err := filter.ParseAction("drop")
	require.Error(t, err)

	_, err = filter.ParseMatchKind("suffix")
	require.Error(t, err)
}
