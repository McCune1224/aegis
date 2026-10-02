package api_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
) // wipeCounts is what one reset reports: the rows each named area gave up, so a
// caller learns what moved instead of inferring it from empty lists.
type wipeCounts struct {
	Clients     int `json:"clients"`
	Discoveries int `json:"discoveries"`
	Queries     int `json:"queries"`
	Services    int `json:"services"`
	Settings    int `json:"settings"`
}

// rowCount counts the entries of a body that is either a JSON array or an
// object whose named key holds one.
func rowCount(t *testing.T, body, key string) int {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &object); err == nil {
		raw, held := object[key]
		require.True(t, held, "no %q in %s", key, body)
		var rows []json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &rows), body)
		return len(rows)
	}
	var rows []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(body), &rows), body)
	return len(rows)
}

// TestResetWipesTheFiveAreasBehindItsConfirmation is the full-wipe contract:
// the endpoint refuses without its confirmation word, empties the areas an
// operator asked to wipe, and leaves a server that still serves, still blocks,
// still saves, and can be wiped again.
func TestResetWipesTheFiveAreasBehindItsConfirmation(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	// One saved record in every area the wipe names.
	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/access", `{"allowed":["10.9.9.0/24"],"disallowed":["10.9.8.0/24"]}`)
	require.Equal(t, http.StatusOK, status, body)
	mac, err := net.ParseMAC("aa:bb:cc:00:00:42")
	require.NoError(t, err)
	require.NoError(t, h.database.RecordDiscovery(t.Context(), store.Discovery{
		MAC:      mac,
		Address:  netip.MustParseAddr("10.9.9.77"),
		Hostname: "livingroom-tv",
		First:    time.Unix(1_700_000_000, 0),
		Last:     time.Unix(1_700_000_600, 0),
	}))
	logged, err := filter.ParseDomain("ads.example.com")
	require.NoError(t, err)
	require.NoError(t, h.database.RecordQueries(t.Context(), []store.QueryEntry{{
		Time:    time.Unix(1_700_000_000, 0),
		Client:  netip.MustParseAddr("127.0.0.1"),
		Name:    logged,
		Type:    "A",
		Verdict: filter.ActionAllow,
	}}))

	// A schedule and a window: a window names a client and a service, so it
	// cannot outlive the wipe and still validate.
	status, body = h.do(t, http.MethodPut, "/api/v1/schedules/night",
		`{"priority":1,"windows":[{"days":[1],"start":"21:00","end":"23:00"}]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/windows/late",
		`{"schedule":"night","clients":["tablet"],"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	// Without the confirmation word nothing is touched.
	status, body = h.do(t, http.MethodPost, "/api/v1/reset", `{}`)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "confirm")
	_, clients := h.do(t, http.MethodGet, "/api/v1/clients", "")
	require.Equal(t, 3, rowCount(t, clients, "clients"))
	_, services := h.do(t, http.MethodGet, "/api/v1/services", "")
	require.Equal(t, 2, rowCount(t, services, "services"))

	// With it the five areas go, and the counts say what moved.
	status, body = h.do(t, http.MethodPost, "/api/v1/reset", `{"confirm":"reset"}`)
	require.Equal(t, http.StatusOK, status, body)
	var counts wipeCounts
	require.NoError(t, json.Unmarshal([]byte(body), &counts), body)
	require.Equal(t, 3, counts.Clients)
	require.Equal(t, 1, counts.Discoveries)
	require.Equal(t, 1, counts.Queries)
	require.Equal(t, 2, counts.Services, "the stored catalog goes with the enablements that name it")
	require.GreaterOrEqual(t, counts.Settings, 2, "both access lists are settings")

	_, clients = h.do(t, http.MethodGet, "/api/v1/clients", "")
	require.Equal(t, 0, rowCount(t, clients, "clients"))
	_, services = h.do(t, http.MethodGet, "/api/v1/services", "")
	require.Equal(t, 0, rowCount(t, services, "services"))
	_, queries := h.do(t, http.MethodGet, "/api/v1/queries", "")
	require.Equal(t, 0, rowCount(t, queries, "queries"))
	_, discoveries := h.do(t, http.MethodGet, "/api/v1/discoveries", "")
	require.Equal(t, 0, rowCount(t, discoveries, "discoveries"))
	_, access := h.do(t, http.MethodGet, "/api/v1/access", "")
	require.Contains(t, access, `"allowed":[]`)
	require.Contains(t, access, `"disallowed":[]`)
	_, windows := h.do(t, http.MethodGet, "/api/v1/windows", "")
	require.Equal(t, 0, rowCount(t, windows, "windows"))
	_, fallback := h.do(t, http.MethodGet, "/api/v1/default-profile", "")
	require.Contains(t, fallback, `"default"`)

	// The server still serves and still blocks: the boot blocklist file
	// outlives the wipe, and a name nothing covers still reaches upstream.
	require.Equal(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.1", h.dnsAddress).Rcode)
	require.NotEqual(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress).Rcode)

	// It still takes writes, so the wiped configuration still validates.
	status, body = h.do(t, http.MethodPut, "/api/v1/rewrites/lamp.local", `{"target":"10.9.9.52"}`)
	require.Equal(t, http.StatusOK, status, body)

	// A service saved after the wipe blocks again, through the fetch, the
	// store, the compile, and a real query on the ephemeral port.
	status, body = h.do(t, http.MethodPost, "/api/v1/services/refresh", "")
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/default/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, mdns.RcodeNameError, queryNamedFrom(t, "127.0.0.1", h.dnsAddress).Rcode)

	// Wiping again is a no-op rather than an error.
	status, body = h.do(t, http.MethodPost, "/api/v1/reset", `{"confirm":"reset"}`)
	require.Equal(t, http.StatusOK, status, body)
	var again wipeCounts
	require.NoError(t, json.Unmarshal([]byte(body), &again), body)
	require.Zero(t, again.Clients)
	require.Zero(t, again.Discoveries)
	require.Equal(t, 1, again.Services, "the refresh put back only what the catalog document carries")
	require.GreaterOrEqual(t, again.Settings, 1, "the bootstrap rows the wipe restores go and come back")
	_, rest := h.do(t, http.MethodGet, "/api/v1/clients", "")
	require.Equal(t, 0, rowCount(t, rest, "clients"))
}
