package services_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/services"
)

const sampleCatalog = `{
  "blocked_services": [
    {
      "id": "youtube",
      "name": "YouTube",
      "rules": ["||youtube.com^", "||youtu.be^"],
      "icon_svg": "<svg/>",
      "group": "streaming"
    },
    {
      "id": "4chan",
      "name": "4chan",
      "rules": ["||4chan.org^"],
      "group": "social_network"
    }
  ],
  "groups": [{"id": "streaming"}, {"id": "social_network"}]
}`

func mustDomain(t *testing.T, raw string) filter.Domain {
	t.Helper()
	parsed, err := filter.ParseDomain(raw)
	require.NoError(t, err)
	return parsed
}

func TestParseCatalogReadsServicesAndGroups(t *testing.T) {
	catalog, err := services.ParseCatalog([]byte(sampleCatalog))
	require.NoError(t, err)

	require.Equal(t, []services.Service{
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^", "||youtu.be^"}},
		{ID: "4chan", Name: "4chan", Group: "social_network", Rules: []string{"||4chan.org^"}},
	}, catalog.Services)
	require.Equal(t, []string{"streaming", "social_network"}, catalog.Groups)
}

func TestParseCatalogAcceptsTheGroupIdSpelling(t *testing.T) {
	body := `{"blocked_services":[{"id":"youtube","name":"YouTube","rules":["||youtube.com^"],"group_id":"streaming"}],"groups":[]}`

	catalog, err := services.ParseCatalog([]byte(body))
	require.NoError(t, err)
	require.Equal(t, "streaming", catalog.Services[0].Group)
}

func TestParseCatalogRejectsWhatItCannotIndex(t *testing.T) {
	cases := map[string]string{
		"broken JSON":              `{"blocked_services":`,
		"a service with no id":     `{"blocked_services":[{"name":"YouTube","rules":[]}]}`,
		"two services with one id": `{"blocked_services":[{"id":"youtube","name":"a","rules":[]},{"id":"youtube","name":"b","rules":[]}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := services.ParseCatalog([]byte(body))
			require.Error(t, err)
		})
	}
}

func TestSpecsTurnCatalogRulesIntoProfileScopedBlockRules(t *testing.T) {
	service := services.Service{
		ID:   "youtube",
		Name: "YouTube",
		Rules: []string{
			"||youtube.com^",
			"|netflix.com.edgesuite.net^",
			"||*.video.example^",
			"/_spotify-connect._tcp.local/",
		},
	}

	specs, skipped := services.Specs(service, "kids")

	require.Equal(t, 0, skipped)
	require.Equal(t, []filter.RuleSpec{
		{
			ID:      "youtube:1",
			Source:  filter.Source{ID: "youtube", Name: "YouTube"},
			Kind:    filter.MatchSubdomains,
			Domain:  mustDomain(t, "youtube.com"),
			Action:  filter.ActionBlock,
			Profile: "kids",
		},
		{
			ID:      "youtube:2",
			Source:  filter.Source{ID: "youtube", Name: "YouTube"},
			Kind:    filter.MatchExact,
			Domain:  mustDomain(t, "netflix.com.edgesuite.net"),
			Action:  filter.ActionBlock,
			Profile: "kids",
		},
		{
			ID:      "youtube:3",
			Source:  filter.Source{ID: "youtube", Name: "YouTube"},
			Kind:    filter.MatchWildcard,
			Pattern: "*.video.example",
			Action:  filter.ActionBlock,
			Profile: "kids",
		},
		{
			ID:      "youtube:4",
			Source:  filter.Source{ID: "youtube", Name: "YouTube"},
			Kind:    filter.MatchRegex,
			Pattern: "_spotify-connect._tcp.local",
			Action:  filter.ActionBlock,
			Profile: "kids",
		},
	}, specs)
}

func TestSpecsSkipLinesTheDnsDialectCannotExpress(t *testing.T) {
	service := services.Service{
		ID:   "spotify",
		Name: "Spotify",
		Rules: []string{
			"example.com##.ad",
			"||spotify.com^",
			"ebay-*.s3-us-west-1.amazonaws.com^",
			"",
		},
	}

	specs, skipped := services.Specs(service, "kids")

	require.Equal(t, 3, skipped)
	require.Len(t, specs, 1)
	require.Equal(t, "spotify.com", specs[0].Domain.String())
	require.Equal(t, filter.MatchSubdomains, specs[0].Kind)
}
