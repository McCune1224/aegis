package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/store"
)

func TestThreatFindingsRoundTripTheirEvidence(t *testing.T) {
	s := open(t)
	when := time.Unix(1_700_000_100, 0).UTC()

	batch := []store.ThreatFinding{
		{
			Time:     when,
			Client:   "phone",
			Kind:     "dga",
			Summary:  "asked for 16 generated-looking domains in 10m0s",
			Evidence: []string{"xkvzwmqpoierutti.biz", "a3f9c2e5b8d7f1a2.biz"},
		},
		{
			Time:     when.Add(time.Minute),
			Client:   "laptop",
			Kind:     "beacon",
			Summary:  "queried c2.example.biz 8 times at 60s mean intervals with 0% jitter",
			Evidence: []string{"60s", "60s", "60s"},
		},
	}
	require.NoError(t, s.RecordThreatFindings(t.Context(), batch))

	findings, err := s.ThreatFindings(t.Context(), 10)

	require.NoError(t, err)
	require.Len(t, findings, 2)
	require.Equal(t, when.Add(time.Minute), findings[0].Time.UTC())
	require.Equal(t, "laptop", findings[0].Client)
	require.Equal(t, "beacon", findings[0].Kind)
	require.Equal(t, []string{"60s", "60s", "60s"}, findings[0].Evidence)
	require.Equal(t, when, findings[1].Time.UTC())
	require.Equal(t, []string{"xkvzwmqpoierutti.biz", "a3f9c2e5b8d7f1a2.biz"}, findings[1].Evidence)
}

func TestThreatFindingsTrimKeepsTheNewest(t *testing.T) {
	s := open(t)

	var batch []store.ThreatFinding
	for i := 0; i < store.ThreatFindingLimit+50; i++ {
		batch = append(batch, store.ThreatFinding{
			Time:    time.Unix(1_700_000_000+int64(i), 0).UTC(),
			Client:  "phone",
			Kind:    "dga",
			Summary: "finding",
		})
	}
	require.NoError(t, s.RecordThreatFindings(t.Context(), batch))
	require.NoError(t, s.TrimThreatFindings(t.Context()))

	findings, err := s.ThreatFindings(t.Context(), store.ThreatFindingLimit+10)

	require.NoError(t, err)
	require.Len(t, findings, store.ThreatFindingLimit)
	require.Equal(t, "finding", findings[0].Summary)
	require.Equal(t, time.Unix(1_700_000_000+int64(store.ThreatFindingLimit)+49, 0).UTC(), findings[0].Time.UTC())
}

func TestThreatFeedsRoundTrip(t *testing.T) {
	s := open(t)

	require.NoError(t, s.SaveThreatFeed(t.Context(), store.ThreatFeed{Name: "urlhaus", URL: "https://example.test/urlhaus.txt", Enabled: true}))
	require.NoError(t, s.SaveThreatFeed(t.Context(), store.ThreatFeed{Name: "urlhaus", URL: "https://example.test/urlhaus-v2.txt", Enabled: false}))

	feeds, err := s.ThreatFeeds(t.Context())

	require.NoError(t, err)
	require.Equal(t, []store.ThreatFeed{{Name: "urlhaus", URL: "https://example.test/urlhaus-v2.txt", Enabled: false}}, feeds)

	require.NoError(t, s.ReplaceThreatDomains(t.Context(), "urlhaus", []store.ThreatDomain{
		{Domain: "malware.example", Kind: "malware_download", Feed: "urlhaus"},
		{Domain: "phish.example", Kind: "phishing", Feed: "urlhaus"},
	}))

	domains, err := s.ThreatDomains(t.Context())

	require.NoError(t, err)
	require.Len(t, domains, 2)

	require.NoError(t, s.DeleteThreatFeed(t.Context(), "urlhaus"))

	domains, err = s.ThreatDomains(t.Context())

	require.NoError(t, err)
	require.Empty(t, domains, "the feed's domains go with it")

	feeds, err = s.ThreatFeeds(t.Context())

	require.NoError(t, err)
	require.Empty(t, feeds)
}
