package blocklist_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
)

var source = filter.Source{ID: "test", Name: "Test list"}

func mustParse(name string) filter.Domain {
	d, err := filter.ParseDomain(name)
	if err != nil {
		panic(err)
	}
	return d
}

func rule(id, name string, action filter.Action) filter.RuleSpec {
	return filter.RuleSpec{
		ID:     id,
		Source: source,
		Kind:   filter.MatchSubdomains,
		Domain: mustParse(name),
		Action: action,
	}
}

func TestParseListReadsHostsEntriesWithAndWithoutAnAddress(t *testing.T) {
	const fixture = `# comment
127.0.0.1 localhost
0.0.0.0 ads.example.com
0.0.0.0 tracker.example.com  # trailing comment
::1 v6.example.com
bare.example.com
`

	got, err := blocklist.ParseList(strings.NewReader(fixture), source, blocklist.FormatHosts)

	require.NoError(t, err)
	require.Equal(t, []filter.RuleSpec{
		rule("test:2", "localhost", filter.ActionBlock),
		rule("test:3", "ads.example.com", filter.ActionBlock),
		rule("test:4", "tracker.example.com", filter.ActionBlock),
		rule("test:5", "v6.example.com", filter.ActionBlock),
		rule("test:6", "bare.example.com", filter.ActionBlock),
	}, got.Rules)
	require.Equal(t, 1, got.Skipped)
}

func TestParseListReadsOneDomainPerLineAndCountsWhatItCannotUse(t *testing.T) {
	const fixture = `# comment
example.com
ads.example.com   # trailing comment
*.wildcard.example
`

	got, err := blocklist.ParseList(strings.NewReader(fixture), source, blocklist.FormatDomains)

	require.NoError(t, err)
	require.Equal(t, []filter.RuleSpec{
		rule("test:2", "example.com", filter.ActionBlock),
		rule("test:3", "ads.example.com", filter.ActionBlock),
	}, got.Rules)
	require.Equal(t, 2, got.Skipped)
}

func TestParseListReadsAdBlockNetworkRulesAndCountsTheCosmeticOnes(t *testing.T) {
	const fixture = `! comment
[Adblock Plus 2.0]
||ads.example.com^
@@||allowed.example.com^
||tracker.example.com^$third-party
example.com##.advert
/regex.*/
||bad domain^
`

	got, err := blocklist.ParseList(strings.NewReader(fixture), source, blocklist.FormatAdBlock)

	require.NoError(t, err)
	require.Equal(t, []filter.RuleSpec{
		rule("test:3", "ads.example.com", filter.ActionBlock),
		rule("test:4", "allowed.example.com", filter.ActionAllow),
		rule("test:5", "tracker.example.com", filter.ActionBlock),
	}, got.Rules)
	require.Equal(t, 5, got.Skipped)
}

func TestParseListGivesEachHostnameOnALineItsOwnRule(t *testing.T) {
	const fixture = `0.0.0.0 first.example.com second.example.com third.example.com
`

	got, err := blocklist.ParseList(strings.NewReader(fixture), source, blocklist.FormatHosts)

	require.NoError(t, err)
	require.Equal(t, []filter.RuleSpec{
		rule("test:1", "first.example.com", filter.ActionBlock),
		rule("test:1.1", "second.example.com", filter.ActionBlock),
		rule("test:1.2", "third.example.com", filter.ActionBlock),
	}, got.Rules)
	require.Equal(t, 0, got.Skipped)
}

func TestParseListReturnsNothingForAnEmptyList(t *testing.T) {
	got, err := blocklist.ParseList(strings.NewReader(""), source, blocklist.FormatDomains)

	require.NoError(t, err)
	require.Empty(t, got.Rules)
	require.Equal(t, 0, got.Skipped)
}

func TestParseListRejectsAnUnknownFormat(t *testing.T) {
	_, err := blocklist.ParseList(strings.NewReader("example.com\n"), source, blocklist.Format(99))

	require.Error(t, err)
}

func TestParsedListDecidesAQueryAndNamesTheLineThatDecidedIt(t *testing.T) {
	const fixture = `! comment
||ads.example.com^
@@||news.ads.example.com^
`

	parsed, err := blocklist.ParseList(strings.NewReader(fixture), source, blocklist.FormatAdBlock)
	require.NoError(t, err)

	set, err := filter.Compile(parsed.Rules)
	require.NoError(t, err)

	engine := filter.New()
	engine.Publish(set)

	blocked := engine.Decide(mustParse("ads.example.com"))
	require.Equal(t, filter.ActionBlock, blocked.Action)
	require.NotNil(t, blocked.Match)
	require.Equal(t, "test:2", blocked.Match.RuleID)
	require.Equal(t, "Test list", blocked.Match.Source.Name)

	allowed := engine.Decide(mustParse("news.ads.example.com"))
	require.Equal(t, filter.ActionAllow, allowed.Action)
	require.NotNil(t, allowed.Match)
	require.Equal(t, "test:3", allowed.Match.RuleID)
}

func TestParseFormatRoundTripsWithString(t *testing.T) {
	for _, name := range []string{"hosts", "domains", "adblock"} {
		format, err := blocklist.ParseFormat(name)
		require.NoError(t, err, "name=%q", name)
		require.Equal(t, name, format.String())
	}
}

func TestParseFormatRejectsAnUnknownName(t *testing.T) {
	_, err := blocklist.ParseFormat("csv")

	require.Error(t, err)
}

func TestParseListRejectsAHostsLineWithNoAddressAndSeveralFields(t *testing.T) {
	const fixture = `0.0.0.0 ads.example.com
not a domain at all
localhost
`

	got, err := blocklist.ParseList(strings.NewReader(fixture), source, blocklist.FormatHosts)

	require.NoError(t, err)
	require.Equal(t, []filter.RuleSpec{
		rule("test:1", "ads.example.com", filter.ActionBlock),
		rule("test:3", "localhost", filter.ActionBlock),
	}, got.Rules)
	require.Equal(t, 1, got.Skipped)
}
