package api_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// TestAnAllowWindowExemptsAServiceOnlyDuringItsSchedule is the merged model:
// a profile's services block always, and a window named allow takes that block
// off one client while its schedule holds, without touching the other clients
// on the same profile.
func TestAnAllowWindowExemptsAServiceOnlyDuringItsSchedule(t *testing.T) {
	noon := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	night := time.Date(2026, 9, 21, 22, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(noon.Unix())
	h := startHarnessWithClock(t, func() time.Time { return time.Unix(clock.Load(), 0).UTC() })

	status, body := h.do(t, http.MethodPost, "/api/v1/services/refresh", "")
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{"mode":"nxdomain"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/laptop", `{"profile":"kids","addresses":["127.0.0.3"]}`)
	require.Equal(t, http.StatusOK, status, body)

	// The profile blocks youtube for both clients, always.
	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/schedules/nightly",
		`{"priority":1,"windows":[{"days":[1],"start":"21:00","end":"23:00"}]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/windows/video-time",
		`{"action":"allow","schedule":"nightly","clients":["tablet"],"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/windows", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"video-time"`)
	require.Contains(t, body, `"action":"allow"`)

	// Outside the schedule the profile block holds for both clients.
	require.Equal(t, mdns.RcodeNameError, queryNamedFrom(t, "127.0.0.2", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeNameError, queryNamedFrom(t, "127.0.0.3", h.dnsAddress).Rcode)

	clock.Store(night.Unix())

	// Inside the schedule only the window's client is exempt: the dead
	// upstream answers, so the wire shows SERVFAIL instead of NXDOMAIN.
	require.Equal(t, mdns.RcodeServerFailure, queryNamedFrom(t, "127.0.0.2", h.dnsAddress).Rcode,
		"the allow window exempts its client")
	require.Equal(t, mdns.RcodeNameError, queryNamedFrom(t, "127.0.0.3", h.dnsAddress).Rcode,
		"a client the window does not name stays blocked")
}

// TestAWindowWithoutAnActionBehavesAsBlock keeps the focus-window semantics
// for a window that states no direction.
func TestAWindowWithoutAnActionBehavesAsBlock(t *testing.T) {
	noon := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	night := time.Date(2026, 9, 21, 22, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(noon.Unix())
	h := startHarnessWithClock(t, func() time.Time { return time.Unix(clock.Load(), 0).UTC() })

	status, body := h.do(t, http.MethodPost, "/api/v1/services/refresh", "")
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"default","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/schedules/nightly",
		`{"priority":1,"windows":[{"days":[1],"start":"21:00","end":"23:00"}]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/windows/school-nights",
		`{"schedule":"nightly","clients":["tablet"],"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/windows/school-nights", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"action":"block"`)

	require.Equal(t, mdns.RcodeServerFailure, queryNamedFrom(t, "127.0.0.2", h.dnsAddress).Rcode)
	clock.Store(night.Unix())
	require.Equal(t, mdns.RcodeNameError, queryNamedFrom(t, "127.0.0.2", h.dnsAddress).Rcode)
}

func TestWindowInputIsRejectedWithFourHundred(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/schedules/night",
		`{"priority":1,"windows":[{"days":[1],"start":"21:00","end":"23:00"}]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"default","addresses":["127.0.0.9"]}`)
	require.Equal(t, http.StatusOK, status, body)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"no schedule", `{"clients":["tablet"],"services":["youtube"]}`, "schedule"},
		{"ghost schedule", `{"schedule":"ghost","clients":["tablet"],"services":["youtube"]}`, "ghost"},
		{"no client", `{"schedule":"night","services":["youtube"]}`, "client"},
		{"ghost client", `{"schedule":"night","clients":["ghost"],"services":["youtube"]}`, "ghost"},
		{"no service", `{"schedule":"night","clients":["tablet"]}`, "service"},
		{"ghost service", `{"schedule":"night","clients":["tablet"],"services":["ghost"]}`, "catalog"},
		{"unknown action", `{"action":"maybe","schedule":"night","clients":["tablet"],"services":["youtube"]}`, "action"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			status, body := h.do(t, http.MethodPut, "/api/v1/windows/bad", testCase.body)
			require.Equal(t, http.StatusBadRequest, status, body)
			require.Contains(t, body, testCase.want)
		})
	}
}
