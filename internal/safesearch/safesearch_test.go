package safesearch_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/safesearch"
)

func mustDomain(t *testing.T, raw string) filter.Domain {
	t.Helper()
	parsed, err := filter.ParseDomain(raw)
	require.NoError(t, err)
	return parsed
}

const fixture = `# AdGuard Search Engine Safe Search Rewrites
#
# Google
|www.google.com^$dnsrewrite=NOERROR;CNAME;forcesafesearch.google.com
|www.google.co.uk^$dnsrewrite=NOERROR;CNAME;forcesafesearch.google.com
#
# Bing
|www.bing.com^$dnsrewrite=NOERROR;CNAME;strict.bing.com
`

func TestParseGroupsRulesByTheEngineHeader(t *testing.T) {
	engines, err := safesearch.Parse(strings.NewReader(fixture), "")
	require.NoError(t, err)

	require.Equal(t, []safesearch.Engine{
		{
			ID:   "google",
			Name: "Google",
			Rules: []safesearch.Rule{
				{Host: mustDomain(t, "www.google.com"), Target: mustDomain(t, "forcesafesearch.google.com")},
				{Host: mustDomain(t, "www.google.co.uk"), Target: mustDomain(t, "forcesafesearch.google.com")},
			},
		},
		{
			ID:    "bing",
			Name:  "Bing",
			Rules: []safesearch.Rule{{Host: mustDomain(t, "www.bing.com"), Target: mustDomain(t, "strict.bing.com")}},
		},
	}, engines)
}

func TestParsePutsAHeaderlessListUnderTheFallbackEngine(t *testing.T) {
	body := "# AdGuard YouTube Safe Search Rewrites\n#\n|www.youtube.com^$dnsrewrite=NOERROR;CNAME;restrictmoderate.youtube.com\n"

	engines, err := safesearch.Parse(strings.NewReader(body), "youtube")
	require.NoError(t, err)

	require.Equal(t, []safesearch.Engine{
		{
			ID:    "youtube",
			Name:  "youtube",
			Rules: []safesearch.Rule{{Host: mustDomain(t, "www.youtube.com"), Target: mustDomain(t, "restrictmoderate.youtube.com")}},
		},
	}, engines)
}

func TestParseSkipsLinesItCannotExpress(t *testing.T) {
	body := `# Google
|www.google.com^
ads.example.com
||ads.example.com^$dnsrewrite=NOERROR;CNAME;forcesafesearch.google.com
|www.google.com^$dnsrewrite=NOERROR;AAAA;::1
|www.google.com^$dnsrewrite=NOERROR;CNAME;forcesafesearch.google.com
`

	engines, err := safesearch.Parse(strings.NewReader(body), "")
	require.NoError(t, err)

	require.Equal(t, []safesearch.Engine{
		{
			ID:    "google",
			Name:  "Google",
			Rules: []safesearch.Rule{{Host: mustDomain(t, "www.google.com"), Target: mustDomain(t, "forcesafesearch.google.com")}},
		},
	}, engines)
}

func TestCatalogCarriesTheEmbeddedEngineLists(t *testing.T) {
	catalog := safesearch.Catalog()

	ids := make([]safesearch.EngineID, 0, len(catalog))
	for _, engine := range catalog {
		ids = append(ids, engine.ID)
	}
	require.Equal(t, []safesearch.EngineID{
		"bing", "brave", "duckduckgo", "ecosia", "google", "pixabay", "qwant", "yandex", "youtube",
	}, ids)

	require.Contains(t, rules(t, catalog, "google"), safesearch.Rule{
		Host:   mustDomain(t, "www.google.com"),
		Target: mustDomain(t, "forcesafesearch.google.com"),
	})
	require.Contains(t, rules(t, catalog, "youtube"), safesearch.Rule{
		Host:   mustDomain(t, "www.youtube.com"),
		Target: mustDomain(t, "restrictmoderate.youtube.com"),
	})
	require.Contains(t, rules(t, catalog, "duckduckgo"), safesearch.Rule{
		Host:   mustDomain(t, "duckduckgo.com"),
		Target: mustDomain(t, "safe.duckduckgo.com"),
	})
}

func rules(t *testing.T, catalog []safesearch.Engine, id safesearch.EngineID) []safesearch.Rule {
	t.Helper()
	for _, engine := range catalog {
		if engine.ID == id {
			return engine.Rules
		}
	}
	t.Fatalf("engine %q is not in the catalog", id)
	return nil
}

func TestTableAnswersOnlyForProfilesThatEnabledTheEngine(t *testing.T) {
	table, err := safesearch.New([]safesearch.Enable{{Profile: "kids", Engine: "google"}}, safesearch.Catalog())
	require.NoError(t, err)

	record, ok := table.Lookup(mustDomain(t, "www.google.com"), "kids")
	require.True(t, ok)
	require.Equal(t, "forcesafesearch.google.com", record.CName.String())

	_, ok = table.Lookup(mustDomain(t, "www.google.com"), "default")
	require.False(t, ok)

	_, ok = table.Lookup(mustDomain(t, "www.bing.com"), "kids")
	require.False(t, ok)
}

func TestNewRejectsAnEngineTheCatalogDoesNotHold(t *testing.T) {
	_, err := safesearch.New([]safesearch.Enable{{Profile: "kids", Engine: "altavista"}}, safesearch.Catalog())

	require.ErrorContains(t, err, "altavista")
}
