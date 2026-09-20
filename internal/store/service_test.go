package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/services"
	"aegis/internal/store"
)

func TestServicesRoundTripTheirRules(t *testing.T) {
	s := open(t)
	fetched := time.Unix(1_700_000_100, 0).UTC()

	require.NoError(t, s.SaveCatalog(t.Context(), fetched, []services.Service{
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^", "||youtu.be^"}},
		{ID: "4chan", Name: "4chan", Group: "social_network", Rules: []string{"||4chan.org^"}},
	}))

	got, err := s.Services(t.Context())

	require.NoError(t, err)
	require.Equal(t, []store.BlockedService{
		{ID: "4chan", Name: "4chan", Group: "social_network", Rules: []string{"||4chan.org^"}, FetchedAt: fetched},
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^", "||youtu.be^"}, FetchedAt: fetched},
	}, got)
}

func TestACatalogThatDropsAServiceKeepsTheStoredCopy(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveCatalog(ctx, time.Unix(1_700_000_100, 0), []services.Service{
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^"}},
	}))

	require.NoError(t, s.SaveCatalog(ctx, time.Unix(1_700_000_200, 0), []services.Service{
		{ID: "4chan", Name: "4chan", Group: "social_network", Rules: []string{"||4chan.org^"}},
	}))

	got, err := s.Services(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, []string{"||youtube.com^"}, got[1].Rules)
}

func TestProfileServicesReplaceTheWholeSet(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "adults"}))
	require.NoError(t, s.SaveCatalog(ctx, time.Unix(1_700_000_100, 0), []services.Service{
		{ID: "youtube", Name: "YouTube", Rules: []string{"||youtube.com^"}},
		{ID: "4chan", Name: "4chan", Rules: []string{"||4chan.org^"}},
	}))

	require.NoError(t, s.SetProfileServices(ctx, "kids", []string{"youtube", "4chan"}))
	require.NoError(t, s.SetProfileServices(ctx, "adults", []string{"4chan"}))
	require.NoError(t, s.SetProfileServices(ctx, "kids", []string{"4chan"}))

	got, err := s.ProfileServices(ctx)

	require.NoError(t, err)
	require.Equal(t, []store.ProfileService{
		{Profile: "adults", Service: "4chan"},
		{Profile: "kids", Service: "4chan"},
	}, got)
}

func TestProfileServicesGoWithTheProfile(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SaveCatalog(ctx, time.Unix(1_700_000_100, 0), []services.Service{
		{ID: "youtube", Name: "YouTube", Rules: []string{"||youtube.com^"}},
	}))
	require.NoError(t, s.SetProfileServices(ctx, "kids", []string{"youtube"}))

	require.NoError(t, s.DeleteProfile(ctx, "kids"))

	got, err := s.ProfileServices(ctx)
	require.NoError(t, err)
	require.Empty(t, got)
}
