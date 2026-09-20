package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

type accessJSON struct {
	Allowed    []string `json:"allowed"`
	Disallowed []string `json:"disallowed"`
}

func TestAccessSetsRoundTripOverHTTP(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/access", `{"allowed":["10.9.9.0/24"],"disallowed":["192.168.9.2/32"]}`)
	require.Equal(t, http.StatusOK, status, body)
	var saved accessJSON
	require.NoError(t, json.Unmarshal([]byte(body), &saved))
	require.Equal(t, []string{"10.9.9.0/24"}, saved.Allowed)
	require.Equal(t, []string{"192.168.9.2/32"}, saved.Disallowed)

	status, body = h.do(t, http.MethodGet, "/api/v1/access", "")
	require.Equal(t, http.StatusOK, status, body)
	var listed accessJSON
	require.NoError(t, json.Unmarshal([]byte(body), &listed))
	require.Equal(t, saved, listed)

	status, body = h.do(t, http.MethodPut, "/api/v1/access", `{"allowed":[],"disallowed":[]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/access", "")
	require.Equal(t, http.StatusOK, status, body)
	listed = accessJSON{}
	require.NoError(t, json.Unmarshal([]byte(body), &listed))
	require.Equal(t, accessJSON{Allowed: []string{}, Disallowed: []string{}}, listed)
}

func TestAccessInputIsRejectedWithFourHundred(t *testing.T) {
	h := startHarness(t)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"unparseable prefix", `{"allowed":["nope"]}`, "nope"},
		{"host bits set", `{"allowed":["10.9.9.5/24"]}`, "host bits"},
		{"listed in both sets", `{"allowed":["10.9.9.0/24"],"disallowed":["10.9.9.0/24"]}`, "10.9.9.0/24"},
		{"malformed json", `{`, "invalid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := h.do(t, http.MethodPut, "/api/v1/access", tc.body)
			require.Equal(t, http.StatusBadRequest, status, body)
			require.Contains(t, body, tc.want)
		})
	}

	status, body := h.do(t, http.MethodGet, "/api/v1/access", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, "[]", "a rejected write changed nothing")
}

func TestAnAccessEditRefusesAClientWithoutARestart(t *testing.T) {
	h := startHarness(t)
	live := startStub(t, "203.0.113.10")

	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+live.address+`","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodDelete, "/api/v1/upstreams/"+deadUpstream, "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/access", `{"allowed":[],"disallowed":["127.0.0.1/32"]}`)
	require.Equal(t, http.StatusOK, status, body)

	got := askName(t, h.dnsAddress, "denied.example.com.")
	require.Equal(t, mdns.RcodeRefused, got.Rcode, "the deny is live on the same server")
	require.EqualValues(t, 0, live.attempts.Load(), "a refused query never reaches the resolver")

	status, body = h.do(t, http.MethodPut, "/api/v1/access", `{"allowed":[],"disallowed":[]}`)
	require.Equal(t, http.StatusOK, status, body)
	got = askName(t, h.dnsAddress, "served.example.com.")
	require.Equal(t, mdns.RcodeSuccess, got.Rcode, "removing the deny is live too")
	require.EqualValues(t, 1, live.attempts.Load())

	status, body = h.do(t, http.MethodPut, "/api/v1/access", `{"allowed":["10.0.0.0/8"],"disallowed":[]}`)
	require.Equal(t, http.StatusOK, status, body)
	got = askName(t, h.dnsAddress, "stranger.example.com.")
	require.Equal(t, mdns.RcodeRefused, got.Rcode, "the permit-list refuses a stranger")

	status, body = h.do(t, http.MethodPut, "/api/v1/access", `{"allowed":["127.0.0.0/8"],"disallowed":[]}`)
	require.Equal(t, http.StatusOK, status, body)
	got = askName(t, h.dnsAddress, "listed.example.com.")
	require.Equal(t, mdns.RcodeSuccess, got.Rcode, "the listed client still resolves")
	require.EqualValues(t, 2, live.attempts.Load())
}
