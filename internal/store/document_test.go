package store_test

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
	"aegis/internal/rewrite"
	"aegis/internal/store"
)

func populateForDocument(t *testing.T, database *store.Store) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, database.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.Refused)}))
	require.NoError(t, database.SaveClient(ctx, store.Client{
		Key:       "tablet",
		Profile:   "kids",
		Notes:     "the spare one",
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.2")},
		Prefixes:  []netip.Prefix{netip.MustParsePrefix("10.9.8.0/24")},
	}))
	require.NoError(t, database.SaveSchedule(ctx, filter.ScheduleSpec{
		Name:     "night",
		Priority: 2,
		Windows:  []filter.Window{{Days: []time.Weekday{time.Monday}, Start: 21 * 60, End: 7 * 60}},
	}))
	_, err := database.SaveRule(ctx, store.Rule{
		Kind:   filter.MatchExact,
		Domain: mustDomain(t, "games.example"),
		Action: filter.ActionBlock,
		Notes:  "handpicked",
	})
	require.NoError(t, err)
	require.NoError(t, database.SaveRewrite(ctx, mustRewrite(t, "nas.example", "10.9.9.77")))
	require.NoError(t, database.SaveUpstream(ctx, store.Upstream{Name: "primary", URL: "9.9.9.9:53", Enabled: true}))
	require.NoError(t, database.SaveUpstream(ctx, store.Upstream{Name: "spare", URL: "9.9.9.10:53", Enabled: true, Backup: true}))
	_, err = database.SaveRoute(ctx, store.Route{Domain: "routed.example", Upstream: "primary"})
	require.NoError(t, err)
	require.NoError(t, database.SetAccess(ctx,
		[]netip.Prefix{netip.MustParsePrefix("10.9.8.0/24")},
		[]netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
	))
	require.NoError(t, database.SaveSource(ctx, store.Source{
		Name:    "local",
		URL:     "file:///tmp/lists/one.txt",
		Format:  blocklist.FormatHosts,
		Enabled: true,
	}))
}

func mustRewrite(t *testing.T, pattern, target string) rewrite.Record {
	t.Helper()
	record, err := rewrite.Parse(pattern, target)
	require.NoError(t, err)
	return record
}

func documentJSON(t *testing.T, database *store.Store) []byte {
	t.Helper()
	document, err := database.ReadDocument(t.Context())
	require.NoError(t, err)
	data, err := json.Marshal(document)
	require.NoError(t, err)
	return data
}

func TestADocumentRoundTripsEverySection(t *testing.T) {
	ctx := t.Context()
	a := open(t)
	populateForDocument(t, a)

	require.NoError(t, a.RecordSourceFetch(ctx, "local", `"etag"`, time.Now(), nil, 5, 0, []byte("0.0.0.0 ads.example.com\n")))

	data := documentJSON(t, a)
	document, err := store.ParseDocument(data)
	require.NoError(t, err)

	b := open(t)
	require.NoError(t, b.ApplyDocument(ctx, document))

	imported, err := b.ReadDocument(ctx)
	require.NoError(t, err)
	exported, err := a.ReadDocument(ctx)
	require.NoError(t, err)
	require.Equal(t, exported, imported)

	cfg, err := b.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
	require.Equal(t, filter.ProfileID("default"), cfg.Default)
	require.Equal(t, []store.Client{{
		Key:       "tablet",
		Profile:   "kids",
		Notes:     "the spare one",
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.2")},
		Prefixes:  []netip.Prefix{netip.MustParsePrefix("10.9.8.0/24")},
	}}, cfg.Clients)
	require.Equal(t, []filter.RuleSpec{{
		ID:     "custom:1",
		Source: filter.Source{ID: "custom", Name: "custom"},
		Kind:   filter.MatchExact,
		Domain: mustDomain(t, "games.example"),
		Action: filter.ActionBlock,
	}}, cfg.Rules)
	require.Len(t, cfg.Rewrites, 1)
	require.Equal(t, "nas.example", cfg.Rewrites[0].Pattern)
	require.Equal(t, netip.MustParseAddr("10.9.9.77"), cfg.Rewrites[0].Addr)
	require.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.9.8.0/24")}, cfg.Allowed)
	require.Equal(t, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, cfg.Disallowed)

	upstreams, err := b.Upstreams(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Upstream{
		{Name: "primary", URL: "udp://9.9.9.9:53", Enabled: true},
		{Name: "spare", URL: "udp://9.9.9.10:53", Enabled: true, Backup: true},
	}, upstreams)

	sources, err := b.Sources(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Source{{
		Name:    "local",
		URL:     "file:///tmp/lists/one.txt",
		Format:  blocklist.FormatHosts,
		Enabled: true,
		Body:    []byte{},
	}}, sources, "the fetch record is operational state and stays out of the document")
}

func TestApplyDocumentUpsertsConflictingRowsAndKeepsTheRest(t *testing.T) {
	ctx := t.Context()
	a := open(t)
	populateForDocument(t, a)

	b := open(t)
	require.NoError(t, b.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.NXDomain)}))
	require.NoError(t, b.SaveProfile(ctx, filter.ProfileSpec{ID: "guests", Mode: modePtr(filter.NXDomain)}))
	require.NoError(t, b.SaveClient(ctx, store.Client{Key: "tablet", Profile: "kids", Notes: "old"}))
	_, err := b.SaveRule(ctx, store.Rule{
		Kind:   filter.MatchExact,
		Domain: mustDomain(t, "other.example"),
		Action: filter.ActionBlock,
	})
	require.NoError(t, err)
	require.NoError(t, b.SaveUpstream(ctx, store.Upstream{Name: "primary", URL: "9.9.9.11:53", Enabled: true}))
	require.NoError(t, b.SaveRewrite(ctx, mustRewrite(t, "keep.example", "10.9.9.9")))
	require.NoError(t, b.SaveSource(ctx, store.Source{
		Name:    "extra",
		URL:     "https://extra.example/list",
		Format:  blocklist.FormatDomains,
		Enabled: true,
	}))

	document, err := store.ParseDocument(documentJSON(t, a))
	require.NoError(t, err)
	require.NoError(t, b.ApplyDocument(ctx, document))

	cfg, err := b.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())

	var kids filter.ProfileSpec
	for _, profile := range cfg.Profiles {
		if profile.ID == "kids" {
			kids = profile
		}
	}
	require.Equal(t, filter.Refused, *kids.Mode, "the document replaces a conflicting profile")
	require.Len(t, cfg.Profiles, 3, "a profile the document never names is kept")
	require.Equal(t, []store.Client{{
		Key:       "tablet",
		Profile:   "kids",
		Notes:     "the spare one",
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.2")},
		Prefixes:  []netip.Prefix{netip.MustParsePrefix("10.9.8.0/24")},
	}}, cfg.Clients)

	rules, err := b.Rules(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Rule{{
		ID:     1,
		Kind:   filter.MatchExact,
		Domain: mustDomain(t, "games.example"),
		Action: filter.ActionBlock,
		Notes:  "handpicked",
	}}, rules, "a rule the document names by id is replaced, not duplicated")

	upstreams, err := b.Upstreams(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Upstream{
		{Name: "primary", URL: "udp://9.9.9.9:53", Enabled: true},
		{Name: "spare", URL: "udp://9.9.9.10:53", Enabled: true, Backup: true},
	}, upstreams)

	require.Len(t, cfg.Rewrites, 2, "a rewrite the document never names is kept")
	sources, err := b.Sources(ctx)
	require.NoError(t, err)
	require.Len(t, sources, 2, "a source the document never names is kept")
}

func TestParseDocumentRefusesAMissingOrUnknownVersion(t *testing.T) {
	_, err := store.ParseDocument([]byte(`{"default_profile": "default"}`))
	require.ErrorContains(t, err, "no version marker")

	_, err = store.ParseDocument([]byte(`{"version": 2}`))
	require.ErrorContains(t, err, "unsupported document version 2")
}
