package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/services"
	"aegis/internal/store"
)

// focusFixture is the smallest configuration a focus window can name: two
// clients on one profile, one schedule, and a catalog with two services.
func focusFixture(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "kids-ipad", Profile: "kids"}))
	require.NoError(t, s.SaveClient(ctx, store.Client{Key: "kids-pc", Profile: "kids"}))
	require.NoError(t, s.SaveSchedule(ctx, filter.ScheduleSpec{
		Name:     "school-nights",
		Priority: 1,
		Windows:  []filter.Window{{Days: []time.Weekday{time.Monday}, Start: 21 * 60, End: 7 * 60}},
	}))
	require.NoError(t, s.SaveCatalog(ctx, time.Unix(1_700_000_100, 0), []services.Service{
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^"}},
		{ID: "4chan", Name: "4chan", Group: "social_network", Rules: []string{"||4chan.org^"}},
	}))
}

func TestFocusWindowsRoundTripAndValidate(t *testing.T) {
	s := open(t)
	focusFixture(t, s)
	ctx := t.Context()

	require.NoError(t, s.SaveFocusWindow(ctx, store.FocusWindow{
		Name:     "school-nights",
		Schedule: "school-nights",
		Clients:  []filter.ClientKey{"kids-ipad", "kids-pc"},
		Services: []string{"4chan", "youtube"},
	}))

	got, err := s.FocusWindows(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.FocusWindow{{
		ID:       1,
		Name:     "school-nights",
		Schedule: "school-nights",
		Clients:  []filter.ClientKey{"kids-ipad", "kids-pc"},
		Services: []string{"4chan", "youtube"},
	}}, got)

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
	require.Equal(t, got, cfg.FocusWindows)
}

func TestSavingAFocusWindowReplacesWhatTheNameHeld(t *testing.T) {
	s := open(t)
	focusFixture(t, s)
	ctx := t.Context()

	require.NoError(t, s.SaveFocusWindow(ctx, store.FocusWindow{
		Name:     "school-nights",
		Schedule: "school-nights",
		Clients:  []filter.ClientKey{"kids-ipad", "kids-pc"},
		Services: []string{"youtube"},
	}))
	require.NoError(t, s.SaveFocusWindow(ctx, store.FocusWindow{
		Name:     "school-nights",
		Schedule: "school-nights",
		Clients:  []filter.ClientKey{"kids-pc"},
		Services: []string{"4chan"},
	}))

	got, err := s.FocusWindows(ctx)

	require.NoError(t, err)
	require.Equal(t, []store.FocusWindow{{
		ID:       1,
		Name:     "school-nights",
		Schedule: "school-nights",
		Clients:  []filter.ClientKey{"kids-pc"},
		Services: []string{"4chan"},
	}}, got)
}

func TestDeletingAFocusWindowRemovesIt(t *testing.T) {
	s := open(t)
	focusFixture(t, s)
	ctx := t.Context()
	require.NoError(t, s.SaveFocusWindow(ctx, store.FocusWindow{
		Name:     "school-nights",
		Schedule: "school-nights",
		Clients:  []filter.ClientKey{"kids-ipad"},
		Services: []string{"youtube"},
	}))

	require.NoError(t, s.DeleteFocusWindow(ctx, "school-nights"))

	got, err := s.FocusWindows(ctx)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestAFocusWindowOnAMissingReferenceIsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		window store.FocusWindow
		want   string
	}{
		{"schedule", store.FocusWindow{Name: "w", Schedule: "ghost", Clients: []filter.ClientKey{"kids-ipad"}, Services: []string{"youtube"}}, "ghost"},
		{"client", store.FocusWindow{Name: "w", Schedule: "school-nights", Clients: []filter.ClientKey{"stranger"}, Services: []string{"youtube"}}, "stranger"},
		{"service", store.FocusWindow{Name: "w", Schedule: "school-nights", Clients: []filter.ClientKey{"kids-ipad"}, Services: []string{"netflix"}}, "netflix"},
		{"no client", store.FocusWindow{Name: "w", Schedule: "school-nights", Services: []string{"youtube"}}, "client"},
		{"no service", store.FocusWindow{Name: "w", Schedule: "school-nights", Clients: []filter.ClientKey{"kids-ipad"}}, "service"},
		{"no schedule", store.FocusWindow{Name: "w", Clients: []filter.ClientKey{"kids-ipad"}, Services: []string{"youtube"}}, "schedule"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			s := open(t)
			focusFixture(t, s)
			cfg, err := s.Load(t.Context())
			require.NoError(t, err)
			cfg.FocusWindows = append(cfg.FocusWindows, testCase.window)

			err = cfg.Validate()

			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.want)
		})
	}
}

func TestTwoFocusWindowsCannotShareAName(t *testing.T) {
	s := open(t)
	focusFixture(t, s)
	cfg, err := s.Load(t.Context())
	require.NoError(t, err)
	window := store.FocusWindow{Name: "w", Schedule: "school-nights", Clients: []filter.ClientKey{"kids-ipad"}, Services: []string{"youtube"}}
	cfg.FocusWindows = append(cfg.FocusWindows, window, window)

	err = cfg.Validate()

	require.Error(t, err)
	require.Contains(t, err.Error(), "twice")
}
