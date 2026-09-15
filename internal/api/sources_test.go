package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// startListServer serves a hosts list with one blocked name.
func startListServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "0.0.0.0 tracker.example.net\n")
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestASourceAddedOverHTTPBlocksARealQuery(t *testing.T) {
	h := startHarness(t)
	url := startListServer(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/sources/trackers", `{"url":"`+url+`/list","format":"hosts","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress, "tracker.example.net.").Rcode)
}

func TestDisablingASourceStopsItBlocking(t *testing.T) {
	h := startHarness(t)
	url := startListServer(t)
	h.do(t, http.MethodPut, "/api/v1/sources/trackers", `{"url":"`+url+`/list","format":"hosts","enabled":true}`)
	require.Equal(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress, "tracker.example.net.").Rcode)

	status, body := h.do(t, http.MethodPut, "/api/v1/sources/trackers", `{"enabled":false}`)
	require.Equal(t, http.StatusOK, status, body)

	require.NotEqual(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress, "tracker.example.net.").Rcode)
}

func TestSourceCRUDReadsBackWhatWasStored(t *testing.T) {
	h := startHarness(t)
	url := startListServer(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/sources/trackers", `{"url":"`+url+`/list","format":"hosts","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"trackers"`)
	require.Contains(t, body, `"format":"hosts"`)
	require.Contains(t, body, `"rule_count":1`)
	require.Contains(t, body, `"enabled":true`)
	require.Contains(t, body, `"last_fetch":`)
	require.NotContains(t, body, "last_error")

	status, body = h.do(t, http.MethodGet, "/api/v1/sources/trackers", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"trackers"`)

	// A two-field body patches: the stored URL and format survive.
	status, body = h.do(t, http.MethodPut, "/api/v1/sources/trackers", `{"enabled":false}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"enabled":false`)
	require.Contains(t, body, url)

	status, body = h.do(t, http.MethodGet, "/api/v1/sources", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"trackers"`)

	status, body = h.do(t, http.MethodDelete, "/api/v1/sources/trackers", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/sources/trackers", "")
	require.Equal(t, http.StatusNotFound, status, body)
	status, body = h.do(t, http.MethodDelete, "/api/v1/sources/trackers", "")
	require.Equal(t, http.StatusNotFound, status, body)
}

func TestAFailedFetchIsShownNotSwallowed(t *testing.T) {
	h := startHarness(t)
	url := startListServer(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/sources/trackers", `{"url":"http://127.0.0.1:1/list","format":"hosts","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"last_error":`)
	require.Contains(t, body, `"rule_count":0`)

	// Pointing the source at a good URL clears the record and the rules land.
	status, body = h.do(t, http.MethodPut, "/api/v1/sources/trackers", `{"url":"`+url+`/list"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.NotContains(t, body, "last_error")
	require.Contains(t, body, `"rule_count":1`)
}

func TestSourceInputIsRejectedWithFourHundred(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown format", `{"url":"http://example.com/l","format":"csv"}`, "format"},
		{"schemeless url", `{"url":"example.com/l"}`, "scheme"},
		{"hostless url", `{"url":"http://"}`, "host"},
		{"missing url", `{"format":"hosts"}`, "url"},
		{"malformed json", `{`, "invalid JSON"},
		{"wrong field type", `{"enabled":"yes"}`, "enabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := startHarness(t)

			status, body := h.do(t, http.MethodPut, "/api/v1/sources/trackers", tc.body)

			require.Equal(t, http.StatusBadRequest, status, body)
			require.Contains(t, body, tc.want)
		})
	}
}

func TestTheCatalogListsKnownSources(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodGet, "/api/v1/sources/catalog", "")

	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"stevenblack"`)
	require.Contains(t, body, `"format":"hosts"`)
}
