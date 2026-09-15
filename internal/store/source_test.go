package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/store"
)

func TestSourcesRoundTripTheirFetchRecord(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	require.NoError(t, s.SaveSource(ctx, store.Source{
		Name:    "stevenblack",
		URL:     "https://example.test/hosts",
		Format:  blocklist.FormatHosts,
		Enabled: true,
	}))
	fetched := time.Unix(1_700_000_000, 0).UTC()
	require.NoError(t, s.RecordSourceFetch(ctx, "stevenblack", "etag-1", fetched, nil, 42, []byte("0.0.0.0 ads.example.com\n")))

	got, err := s.Sources(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Source{{
		Name:      "stevenblack",
		URL:       "https://example.test/hosts",
		Format:    blocklist.FormatHosts,
		Enabled:   true,
		ETag:      "etag-1",
		LastFetch: fetched,
		RuleCount: 42,
		Body:      []byte("0.0.0.0 ads.example.com\n"),
	}}, got)
}

func TestEnabledSourcesSkipsDisabledOnes(t *testing.T) {
	s := open(t)
	ctx := t.Context()

	require.NoError(t, s.SaveSource(ctx, store.Source{Name: "on", URL: "https://example.test/a", Format: blocklist.FormatDomains, Enabled: true}))
	require.NoError(t, s.SaveSource(ctx, store.Source{Name: "off", URL: "https://example.test/b", Format: blocklist.FormatDomains, Enabled: false}))

	got, err := s.EnabledSources(ctx)

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "on", got[0].Name)
}

func TestRecordSourceFetchKeepsTheFailure(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveSource(ctx, store.Source{Name: "broken", URL: "https://example.test/x", Format: blocklist.FormatDomains, Enabled: true}))

	require.NoError(t, s.RecordSourceFetch(ctx, "broken", "", time.Now(), errors.New("dial tcp: no route"), 0, nil))

	got, err := s.Sources(ctx)
	require.NoError(t, err)
	require.Equal(t, "dial tcp: no route", got[0].LastError)
}

func TestSavingASourceKeepsItsFetchRecord(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveSource(ctx, store.Source{Name: "keep", URL: "https://example.test/a", Format: blocklist.FormatDomains, Enabled: true}))
	require.NoError(t, s.RecordSourceFetch(ctx, "keep", "etag", time.Unix(1_700_000_000, 0), nil, 7, []byte("body")))

	require.NoError(t, s.SaveSource(ctx, store.Source{Name: "keep", URL: "https://example.test/b", Format: blocklist.FormatHosts, Enabled: false}))

	got, err := s.Sources(ctx)
	require.NoError(t, err)
	require.Equal(t, "etag", got[0].ETag)
	require.Equal(t, 7, got[0].RuleCount)
	require.False(t, got[0].Enabled)
}
