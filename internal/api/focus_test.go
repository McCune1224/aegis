package api_test

import (
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// queryNamedFrom asks the harness DNS server for one chosen name from a chosen
// source address, so a test can watch one client's view of one service.
func queryNamedFrom(t *testing.T, local, server, name string) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{
		Net:     "udp",
		Timeout: 2 * time.Second,
		Dialer:  &net.Dialer{LocalAddr: &net.UDPAddr{IP: net.ParseIP(local)}},
	}
	resp, _, err := client.Exchange(new(mdns.Msg).SetQuestion(mdns.Fqdn(name), mdns.TypeA), server)
	require.NoError(t, err)
	return resp
}

func TestAFocusWindowBlocksOneClientOnlyDuringItsSchedule(t *testing.T) {
	noon := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	night := time.Date(2026, 9, 21, 22, 0, 0, 0, time.UTC)
	// The DNS server reads the clock on its own goroutine, so the test moves it
	// atomically rather than by writing a captured variable under the race
	// detector.
	var clock atomic.Int64
	clock.Store(noon.Unix())
	h := startHarnessWithClock(t, func() time.Time { return time.Unix(clock.Load(), 0).UTC() })

	status, body := h.do(t, http.MethodPost, "/api/v1/services/refresh", "")
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{"mode":"nxdomain"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/schedules/school-nights",
		`{"priority":1,"windows":[{"days":[1],"start":"21:00","end":"23:00"}]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/focus/school-nights",
		`{"schedule":"school-nights","clients":["tablet"],"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/focus", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"school-nights"`)
	require.Contains(t, body, `"clients":["tablet"]`)
	require.Contains(t, body, `"services":["youtube"]`)

	// Noon is outside the window, so youtube.com reaches the dead upstream and
	// the wire answer is SERVFAIL rather than the blocked NXDOMAIN.
	require.Equal(t, mdns.RcodeServerFailure, queryNamedFrom(t, "127.0.0.2", h.dnsAddress, "youtube.com").Rcode)

	clock.Store(night.Unix())

	require.Equal(t, mdns.RcodeNameError, queryNamedFrom(t, "127.0.0.2", h.dnsAddress, "youtube.com").Rcode,
		"inside the window the named client is blocked")
	require.Equal(t, mdns.RcodeServerFailure, queryNamedFrom(t, "127.0.0.1", h.dnsAddress, "youtube.com").Rcode,
		"a client the window does not name is unaffected")

	// A client a focus window still blocks cannot be deleted out from under it.
	status, body = h.do(t, http.MethodDelete, "/api/v1/clients/tablet", "")
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "school-nights")

	status, body = h.do(t, http.MethodDelete, "/api/v1/schedules/school-nights", "")
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "school-nights")

	status, body = h.do(t, http.MethodDelete, "/api/v1/focus/school-nights", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/schedules/school-nights", "")
	require.Equal(t, http.StatusNoContent, status, body)
}

func TestFocusInputIsRejectedWithFourHundred(t *testing.T) {
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
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			status, body := h.do(t, http.MethodPut, "/api/v1/focus/bad", testCase.body)
			require.Equal(t, http.StatusBadRequest, status, body)
			require.Contains(t, body, testCase.want)
		})
	}
}
