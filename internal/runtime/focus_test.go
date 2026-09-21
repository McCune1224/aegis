package runtime_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/runtime"
	"aegis/internal/services"
	"aegis/internal/store"
)

// focusStore holds two clients on one profile, a school-nights schedule, a
// catalog with YouTube, and a focus window blocking YouTube for the tablet
// only during that schedule.
func focusStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := t.Context()
	s := openStore(t)
	require.NoError(t, s.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, s.SaveClient(ctx, store.Client{
		Key: "tablet", Profile: "kids", Addresses: []netip.Addr{tablet},
	}))
	require.NoError(t, s.SaveClient(ctx, store.Client{
		Key: "laptop", Profile: "kids", Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.3")},
	}))
	require.NoError(t, s.SaveSchedule(ctx, filter.ScheduleSpec{
		Name:     "school-nights",
		Priority: 1,
		Windows:  []filter.Window{{Days: []time.Weekday{time.Monday}, Start: 21 * 60, End: 23 * 60}},
	}))
	require.NoError(t, s.SaveCatalog(ctx, time.Unix(1_700_000_100, 0), []services.Service{
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^"}},
	}))
	require.NoError(t, s.SaveFocusWindow(ctx, store.FocusWindow{
		Name:     "school-nights",
		Schedule: "school-nights",
		Clients:  []filter.ClientKey{"tablet"},
		Services: []string{"youtube"},
	}))
	return s
}

func TestAFocusWindowBlocksOneClientOnlyDuringItsSchedule(t *testing.T) {
	ctx := t.Context()
	mondayNoon := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	mondayNight := time.Date(2026, 9, 21, 22, 0, 0, 0, time.UTC)
	clock := mondayNoon

	rt := runtime.New(focusStore(t), nil, quietLogger(), runtime.WithClock(func() time.Time { return clock }))
	require.NoError(t, rt.Reload(ctx))

	youtube := mustDomain(t, "youtube.com")
	laptop := netip.MustParseAddr("10.9.9.3")

	require.Equal(t, filter.ActionAllow, rt.Decide(youtube, tablet).Action,
		"outside the window the focus window contributes nothing")
	require.Equal(t, filter.ActionAllow, rt.Decide(youtube, laptop).Action)

	clock = mondayNight

	blocked := rt.Decide(youtube, tablet)
	require.Equal(t, filter.ActionBlock, blocked.Action, "the named client is blocked inside the window")
	require.NotNil(t, blocked.Match)
	require.Equal(t, "youtube", blocked.Match.Source.ID, "the verdict names the service")
	require.Equal(t, filter.ActionAllow, rt.Decide(youtube, laptop).Action,
		"a client the window does not name stays unblocked")
}

// TestAFocusWindowIsAdditiveAtTheProfileLevel covers the rule that a window
// only ever adds blocking: the profile's always-on services stay blocked
// outside every window.
func TestAFocusWindowIsAdditiveAtTheProfileLevel(t *testing.T) {
	ctx := t.Context()
	s := focusStore(t)
	require.NoError(t, s.SetProfileServices(ctx, "kids", []string{"youtube"}))
	// A second service the profile blocks always-on, which no window names.
	require.NoError(t, s.SaveCatalog(ctx, time.Unix(1_700_000_200, 0), []services.Service{
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^"}},
		{ID: "4chan", Name: "4chan", Group: "social_network", Rules: []string{"||4chan.org^"}},
	}))
	require.NoError(t, s.SetProfileServices(ctx, "kids", []string{"youtube", "4chan"}))

	mondayNoon := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	rt := runtime.New(s, nil, quietLogger(), runtime.WithClock(func() time.Time { return mondayNoon }))
	require.NoError(t, rt.Reload(ctx))

	require.Equal(t, filter.ActionBlock, rt.Decide(mustDomain(t, "youtube.com"), tablet).Action,
		"a profile service stays blocked outside the window")
	require.Equal(t, filter.ActionBlock, rt.Decide(mustDomain(t, "4chan.org"), tablet).Action,
		"a profile service no window names is blocked too")
}
