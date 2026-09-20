package api_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/api"
	"aegis/internal/blocklist"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

func TestListThreatFindingsServesRecordedFindings(t *testing.T) {
	h := startHarness(t)
	when := time.Unix(1_700_000_100, 0).UTC()

	batch := []store.ThreatFinding{
		{
			Time:     when,
			Client:   "phone",
			Kind:     "dga",
			Summary:  "asked for 16 generated-looking domains in 10m0s",
			Evidence: []string{"xkvzwmqpoierutti.biz"},
		},
	}
	require.NoError(t, h.database.RecordThreatFindings(t.Context(), batch))

	status, body := h.do(t, http.MethodGet, "/api/v1/threats/findings", "")

	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"kind":"dga"`)
	require.Contains(t, body, `"client":"phone"`)
	require.Contains(t, body, `"summary":"asked for 16 generated-looking domains in 10m0s"`)
	require.Contains(t, body, "xkvzwmqpoierutti.biz")
}

func TestListThreatFindingsRejectsABadLimit(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodGet, "/api/v1/threats/findings?limit=zero", "")

	require.Equal(t, http.StatusBadRequest, status, body)
}

type stubRefresher struct {
	refreshed []string
	all       bool
}

func (s *stubRefresher) RefreshFeeds(ctx context.Context) error {
	s.all = true
	return nil
}

func (s *stubRefresher) RefreshFeed(ctx context.Context, name string) error {
	s.refreshed = append(s.refreshed, name)
	return nil
}

func startThreatHarness(t *testing.T) (*harness, *stubRefresher) {
	t.Helper()
	ctx := t.Context()

	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	rt := runtime.New(database, nil, quietLogger())
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(time.Second), rt, quietLogger())
	refresher := &stubRefresher{}
	apiServer, err := api.Start(api.Config{Store: database, Reloader: rt, Sources: sync, Preview: sync, Threats: refresher, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = apiServer.Shutdown(context.Background()) })

	return &harness{apiURL: "http://" + apiServer.Addr().String(), database: database}, refresher
}

func TestThreatFeedsRoundTripOverTheAPI(t *testing.T) {
	h, refresher := startThreatHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/threats/feeds/urlhaus", `{"url":"https://feeds.example/urlhaus.txt"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"enabled":true`)

	status, body = h.do(t, http.MethodGet, "/api/v1/threats/feeds", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, "feeds.example")

	status, body = h.do(t, http.MethodPost, "/api/v1/threats/feeds/urlhaus/refresh", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, []string{"urlhaus"}, refresher.refreshed)

	status, body = h.do(t, http.MethodPut, "/api/v1/threats/feeds/urlhaus", `{"url":"https://feeds.example/urlhaus.txt","enabled":false}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"enabled":false`, "an update with no enabled field keeps the stored value")

	status, body = h.do(t, http.MethodDelete, "/api/v1/threats/feeds/urlhaus", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, _ = h.do(t, http.MethodGet, "/api/v1/threats/feeds", "")
	require.Equal(t, http.StatusOK, status)
}

func TestAPutThreatFeedNeedsAURL(t *testing.T) {
	h, _ := startThreatHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/threats/feeds/empty", `{}`)
	require.Equal(t, http.StatusBadRequest, status, body)
}
