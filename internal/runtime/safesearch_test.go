package runtime_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/rewrite"
	"aegis/internal/runtime"
	"aegis/internal/safesearch"
	"aegis/internal/store"
)

var laptop = netip.MustParseAddr("10.9.9.3")

func TestSafeSearchAnswersOnlyForTheProfileThatEnabledIt(t *testing.T) {
	s := configuredStore(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "adults"}))
	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "laptop", Profile: "adults", Addresses: []netip.Addr{laptop}}))
	require.NoError(t, s.SetProfileSafesearch(ctx, "kids", []safesearch.EngineID{"google"}))

	rt := runtime.New(s, nil, quietLogger())
	require.NoError(t, rt.Reload(ctx))

	record, ok := rt.Lookup(mustDomain(t, "www.google.com"), tablet)
	require.True(t, ok)
	require.Equal(t, "forcesafesearch.google.com", record.CName.String())

	_, ok = rt.Lookup(mustDomain(t, "www.google.com"), laptop)
	require.False(t, ok)

	_, ok = rt.Lookup(mustDomain(t, "www.bing.com"), tablet)
	require.False(t, ok, "an engine the profile did not enable answers nothing")
}

func TestAnOperatorsRewriteBeatsSafeSearch(t *testing.T) {
	s := configuredStore(t)
	ctx := t.Context()
	require.NoError(t, s.SetProfileSafesearch(ctx, "kids", []safesearch.EngineID{"google"}))
	record, err := rewrite.Parse("www.google.com", "10.0.0.99")
	require.NoError(t, err)
	require.NoError(t, s.SaveRewrite(ctx, record))

	rt := runtime.New(s, nil, quietLogger())
	require.NoError(t, rt.Reload(ctx))

	got, ok := rt.Lookup(mustDomain(t, "www.google.com"), tablet)
	require.True(t, ok)
	require.Equal(t, "10.0.0.99", got.Addr.String())
}
