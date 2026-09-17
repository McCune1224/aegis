package api_test

import (
	"net/http"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// pinnedClock reads a Wednesday noon in UTC, so a test states the minute the
// engine answers at and a schedule either covers it or does not.
func pinnedClock(t *testing.T) func() time.Time {
	t.Helper()
	return func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }
}

func TestAScheduleTimesARealRule(t *testing.T) {
	h := startHarnessWithClock(t, pinnedClock(t))

	status, body := h.do(t, http.MethodPut, "/api/v1/schedules/night",
		`{"priority":5,"windows":[{"days":[0,1,2,3,4,5,6],"start":"21:00","end":"23:59"}]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"tracker.example.net","kind":"exact","action":"block","schedule":"night"}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"schedule":"night"`)

	// Noon is outside the window, so the name still resolves.
	require.NotEqual(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress).Rcode)

	status, body = h.do(t, http.MethodGet, "/api/v1/schedules", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"night"`)
	require.Contains(t, body, `"priority":5`)
	require.Contains(t, body, `"start":"21:00"`)

	// Replacing the schedule with one that covers the whole day takes effect
	// on the next query, without touching the rule.
	status, body = h.do(t, http.MethodPut, "/api/v1/schedules/night",
		`{"priority":5,"windows":[{"days":[0,1,2,3,4,5,6],"start":"00:00","end":"23:59"}]}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress).Rcode)

	// The schedule cannot go away while a rule still names it.
	status, body = h.do(t, http.MethodDelete, "/api/v1/schedules/night", "")
	require.Equal(t, http.StatusBadRequest, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/rules/1", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/schedules/night", "")
	require.Equal(t, http.StatusNoContent, status, body)
}

func TestScheduleInputIsRejectedWithFourHundred(t *testing.T) {
	h := startHarness(t)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"no windows", `{"priority":1,"windows":[]}`, "window"},
		{"no days", `{"priority":1,"windows":[{"days":[],"start":"00:00","end":"01:00"}]}`, "day"},
		{"a bad time", `{"priority":1,"windows":[{"days":[1],"start":"25:00","end":"01:00"}]}`, "time of day"},
		{"an empty window", `{"priority":1,"windows":[{"days":[1],"start":"01:00","end":"01:00"}]}`, "minute"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			status, body := h.do(t, http.MethodPut, "/api/v1/schedules/bad", testCase.body)
			require.Equal(t, http.StatusBadRequest, status, body)
			require.Contains(t, body, testCase.want)
		})
	}
}

func TestARuleCannotNameAScheduleThatDoesNotExist(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"example.com","kind":"exact","action":"block","schedule":"ghost"}`)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "ghost")
}
