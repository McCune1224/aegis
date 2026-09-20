package store_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/store"
)

func seedRoutes(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "quad9", URL: "9.9.9.9:53", Enabled: true}))
	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "backup", URL: "tls://8.8.8.8", Enabled: true}))
	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "tablet", Profile: "default"}))
}

func TestRoutesRoundTripThroughTheStore(t *testing.T) {
	s := open(t)
	seedRoutes(t, s)
	ctx := t.Context()

	saved, err := s.SaveRoute(ctx, store.Route{Domain: "Target.Example", Upstream: "backup"})
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.ID)
	require.Equal(t, "target.example", saved.Domain, "the store keeps the lowercase form")

	second, err := s.SaveRoute(ctx, store.Route{Client: "tablet", Upstream: "quad9"})
	require.NoError(t, err)
	require.EqualValues(t, 2, second.ID)

	rows, err := s.Routes(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Route{
		{ID: 1, Domain: "target.example", Upstream: "backup"},
		{ID: 2, Client: "tablet", Upstream: "quad9"},
	}, rows)

	require.NoError(t, s.UpdateRoute(ctx, store.Route{ID: 2, Domain: "other.example", Upstream: "backup"}))
	rows, err = s.Routes(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Route{
		{ID: 1, Domain: "target.example", Upstream: "backup"},
		{ID: 2, Domain: "other.example", Upstream: "backup"},
	}, rows)

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, rows, cfg.Routes)

	require.NoError(t, s.DeleteRoute(ctx, 1))
	rows, err = s.Routes(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Route{{ID: 2, Domain: "other.example", Upstream: "backup"}}, rows)
}

func TestDeletingAnUpstreamRemovesItsRoutes(t *testing.T) {
	s := open(t)
	seedRoutes(t, s)
	ctx := t.Context()

	_, err := s.SaveRoute(ctx, store.Route{Domain: "target.example", Upstream: "backup"})
	require.NoError(t, err)

	require.NoError(t, s.DeleteUpstream(ctx, "backup"))

	rows, err := s.Routes(ctx)
	require.NoError(t, err)
	require.Empty(t, rows, "the foreign key carries the route away with the upstream")
}

func TestValidateRejectsARouteWithNowhereToGo(t *testing.T) {
	s := open(t)
	seedRoutes(t, s)
	require.NoError(t, s.SaveUpstream(t.Context(), store.Upstream{Name: "spare", URL: "9.9.9.9:53", Enabled: false}))

	cases := []struct {
		name  string
		route store.Route
		want  string
	}{
		{"unknown upstream", store.Route{Domain: "x.example", Upstream: "ghost"}, `"ghost"`},
		{"disabled upstream", store.Route{Upstream: "spare"}, "not an enabled resolver"},
		{"unknown client", store.Route{Client: "ghost", Upstream: "quad9"}, `"ghost"`},
		{"malformed domain", store.Route{Domain: "two words", Upstream: "quad9"}, "malformed domain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := s.Load(t.Context())
			require.NoError(t, err)
			cfg.Routes = []store.Route{tc.route}

			err = cfg.Validate()

			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
