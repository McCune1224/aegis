package api_test

import (
	"fmt"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
)

func TestObservedClientsEndpointListsQueryTraffic(t *testing.T) {
	h := startHarness(t)
	hourAgo := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	require.NoError(t, h.database.RecordQueries(t.Context(), []store.QueryEntry{
		{Time: hourAgo, Client: netip.MustParseAddr("192.168.68.50"), Name: mustDomain(t, "a.example"), Type: "A", Verdict: filter.ActionAllow},
		{Time: hourAgo.Add(time.Minute), Client: netip.MustParseAddr("192.168.68.50"), Name: mustDomain(t, "b.example"), Type: "A", Verdict: filter.ActionBlock},
	}))

	status, body := h.do(t, http.MethodGet, "/api/v1/clients/observed", "")
	require.Equal(t, http.StatusOK, status, body)
	require.JSONEq(t, fmt.Sprintf(`{"observed":[
		{"client":"192.168.68.50","queries":2,"last_seen":%d,"claimed":false}
	]}`, hourAgo.Add(time.Minute).UnixMilli()), body)

	status, body = h.do(t, http.MethodGet, "/api/v1/clients/observed?window=1h", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, "192.168.68.50")

	status, body = h.do(t, http.MethodGet, "/api/v1/clients/observed?window=400h", "")
	require.Equal(t, http.StatusBadRequest, status, body)
}
