package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type routeJSON struct {
	ID       int64  `json:"id"`
	Domain   string `json:"domain"`
	Client   string `json:"client"`
	Upstream string `json:"upstream"`
}

func TestARouteRoundTripsOverHTTP(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/routes", `{"domain":"Target.Example.","client":"","upstream":"`+deadUpstream+`"}`)
	require.Equal(t, http.StatusCreated, status, body)
	var created routeJSON
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	require.Equal(t, int64(1), created.ID)
	require.Equal(t, "target.example", created.Domain, "the stored domain is the parsed, lowercased form")
	require.Equal(t, "", created.Client)
	require.Equal(t, deadUpstream, created.Upstream)

	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"default"}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/routes/1", `{"domain":"other.example","client":"tablet","upstream":"`+deadUpstream+`"}`)
	require.Equal(t, http.StatusOK, status, body)
	var updated routeJSON
	require.NoError(t, json.Unmarshal([]byte(body), &updated))
	require.Equal(t, int64(1), updated.ID)
	require.Equal(t, "other.example", updated.Domain)
	require.Equal(t, "tablet", updated.Client)

	status, body = h.do(t, http.MethodGet, "/api/v1/routes", "")
	require.Equal(t, http.StatusOK, status, body)
	var routes []routeJSON
	require.NoError(t, json.Unmarshal([]byte(body), &routes))
	require.Len(t, routes, 1)
	require.Equal(t, updated, routes[0])

	status, body = h.do(t, http.MethodPut, "/api/v1/routes/99", `{"domain":"other.example","upstream":"`+deadUpstream+`"}`)
	require.Equal(t, http.StatusNotFound, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/routes/1", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/routes", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, "[]\n", body, "the deleted route is gone")
}

func TestRouteInputIsRejectedWithFourHundred(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/off", `{"url":"9.9.9.9:53","enabled":false}`)
	require.Equal(t, http.StatusOK, status, body)

	cases := []struct {
		name string
		body string
	}{
		{"unknown upstream", `{"domain":"target.example","upstream":"ghost"}`},
		{"disabled upstream", `{"domain":"target.example","upstream":"off"}`},
		{"unknown client", `{"domain":"target.example","client":"ghost","upstream":"` + deadUpstream + `"}`},
		{"bad domain", `{"domain":"not..a..domain","upstream":"` + deadUpstream + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := h.do(t, http.MethodPost, "/api/v1/routes", tc.body)
			require.Equal(t, http.StatusBadRequest, status, body)
		})
	}
}

func TestAnUpstreamWithARouteCannotBeDeleted(t *testing.T) {
	h := startHarness(t)
	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/spare", `{"url":"9.9.9.9:53","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPost, "/api/v1/routes", `{"domain":"target.example","upstream":"`+deadUpstream+`"}`)
	require.Equal(t, http.StatusCreated, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/upstreams/"+deadUpstream, "")
	require.Equal(t, http.StatusConflict, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/routes/1", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/upstreams/"+deadUpstream, "")
	require.Equal(t, http.StatusNoContent, status, body)
}

func TestARouteEditRedirectsADomainWithoutARestart(t *testing.T) {
	h := startHarness(t)
	a := startStub(t, "203.0.113.10")
	b := startStub(t, "203.0.113.11")

	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+a.address+`","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/upstreams/second", `{"url":"`+b.address+`","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodDelete, "/api/v1/upstreams/"+deadUpstream, "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodPost, "/api/v1/routes", `{"domain":"target.example","client":"","upstream":"second"}`)
	require.Equal(t, http.StatusCreated, status, body)

	got := askName(t, h.dnsAddress, "x.target.example.")
	require.Equal(t, "203.0.113.11", answerAddress(t, got), "the routed domain reaches the second stub")
	require.EqualValues(t, 0, a.attempts.Load(), "a routed query never touches the other resolver")

	got = askName(t, h.dnsAddress, "fresh.example.com.")
	require.Equal(t, "203.0.113.10", answerAddress(t, got), "an unrouted name uses the pool")

	status, body = h.do(t, http.MethodPut, "/api/v1/routes/1", `{"domain":"target.example","client":"","upstream":"primary"}`)
	require.Equal(t, http.StatusOK, status, body)

	got = askName(t, h.dnsAddress, "x2.target.example.")
	require.Equal(t, "203.0.113.10", answerAddress(t, got), "the edit is live on the same server")
	require.EqualValues(t, 1, b.attempts.Load())
}
