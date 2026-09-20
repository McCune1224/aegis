package store_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/safesearch"
	"aegis/internal/store"
)

func TestProfileSafesearchReplacesTheWholeSet(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "adults"}))

	require.NoError(t, s.SetProfileSafesearch(ctx, "kids", []safesearch.EngineID{"google", "youtube"}))
	require.NoError(t, s.SetProfileSafesearch(ctx, "adults", []safesearch.EngineID{"bing"}))
	require.NoError(t, s.SetProfileSafesearch(ctx, "kids", []safesearch.EngineID{"youtube"}))

	got, err := s.ProfileSafesearch(ctx)

	require.NoError(t, err)
	require.Equal(t, []store.ProfileSafesearch{
		{Profile: "adults", Engine: "bing"},
		{Profile: "kids", Engine: "youtube"},
	}, got)
}

func TestProfileSafesearchGoesWithTheProfile(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SetProfileSafesearch(ctx, "kids", []safesearch.EngineID{"google"}))

	require.NoError(t, s.DeleteProfile(ctx, "kids"))

	got, err := s.ProfileSafesearch(ctx)
	require.NoError(t, err)
	require.Empty(t, got)
}
