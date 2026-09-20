package api_test

import (
	"net/http"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
)

func TestARuleAddedOverHTTPBlocksARealQuery(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"tracker.example.net","kind":"subdomains","action":"block","notes":"seen in the wild"}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"id":1`)
	require.Contains(t, body, `"kind":"subdomains"`)
	require.Contains(t, body, `"notes":"seen in the wild"`)
	require.Contains(t, body, `"created":`)

	require.Equal(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress).Rcode)
}

func TestAnAllowRuleAddedOverHTTPUnblocksAListedName(t *testing.T) {
	h := startHarness(t)
	url := startListServer(t)
	status, body := h.do(t, http.MethodPut, "/api/v1/sources/trackers",
		`{"url":"`+url+`/list","format":"hosts","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress).Rcode)

	decisions, stop := h.hub.Subscribe()
	defer stop()

	status, body = h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"tracker.example.net","kind":"exact","action":"allow"}`)
	require.Equal(t, http.StatusCreated, status, body)

	require.NotEqual(t, mdns.RcodeNameError, queryFor(t, h.dnsAddress).Rcode)

	select {
	case got := <-decisions:
		require.Equal(t, listedName, got.Name.String())
		require.Equal(t, filter.ActionAllow, got.Action)
		require.NotNil(t, got.Match)
		require.Equal(t, "custom:1", got.Match.RuleID)
		require.Equal(t, "custom", got.Match.Source.Name)
	case <-time.After(2 * time.Second):
		t.Fatal("no decision reached the stream")
	}
}

func TestRuleCRUDReadsBackWhatWasStored(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"games.example.com","kind":"exact","action":"allow"}`)
	require.Equal(t, http.StatusCreated, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/rules", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"domain":"games.example.com"`)

	// A one-field body patches: the stored domain and kind survive.
	status, body = h.do(t, http.MethodPut, "/api/v1/rules/1", `{"action":"block"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"action":"block"`)
	require.Contains(t, body, `"kind":"exact"`)

	status, body = h.do(t, http.MethodDelete, "/api/v1/rules/1", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/rules", "")
	require.Equal(t, http.StatusOK, status, body)
	require.NotContains(t, body, "games.example.com")

	status, body = h.do(t, http.MethodDelete, "/api/v1/rules/1", "")
	require.Equal(t, http.StatusNotFound, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/rules/1", `{"action":"block"}`)
	require.Equal(t, http.StatusNotFound, status, body)
}

func TestRuleInputIsRejectedWithFourHundred(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown kind", `{"domain":"example.com","kind":"suffix","action":"block"}`, "kind"},
		{"unknown action", `{"domain":"example.com","kind":"exact","action":"drop"}`, "action"},
		{"malformed domain", `{"domain":"example.com/path","kind":"exact","action":"block"}`, "domain"},
		{"missing domain", `{"kind":"exact","action":"block"}`, "domain"},
		{"missing kind", `{"domain":"example.com","action":"block"}`, "kind"},
		{"missing action", `{"domain":"example.com","kind":"exact"}`, "action"},
		{"malformed json", `{`, "invalid JSON"},
		{"wrong field type", `{"domain":1}`, "domain"},
		{"regex that does not compile", `{"domain":"([)+","kind":"regex","action":"block"}`, "regex"},
		{"empty regex", `{"domain":"","kind":"regex","action":"block"}`, "pattern"},
		{"cidr that is not a prefix", `{"domain":"10.0.0.0/99","kind":"cidr","action":"block"}`, "prefix"},
		{"wildcard with a bad label", `{"domain":"a**b.example","kind":"wildcard","action":"block"}`, "wildcard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := startHarness(t)

			status, body := h.do(t, http.MethodPost, "/api/v1/rules", tc.body)

			require.Equal(t, http.StatusBadRequest, status, body)
			require.Contains(t, body, tc.want)
		})
	}
}

func TestRegexAndCIDRRulesGoThroughHTTPAndDecideRealQueries(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"^ads\\.example\\.(com|net)$","kind":"regex","action":"block"}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.5", h.dnsAddress).Rcode, body)

	status, body = h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"127.0.0.5/32","kind":"cidr","action":"allow"}`)
	require.Equal(t, http.StatusCreated, status, body)

	// The network exemption outranks the regex block for that one address,
	// and every other loopback address still sees the block.
	require.NotEqual(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.5", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.6", h.dnsAddress).Rcode)

	status, body = h.do(t, http.MethodGet, "/api/v1/rules", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"kind":"regex"`)
	require.Contains(t, body, `"domain":"^ads\\.example\\.(com|net)$"`)
	require.Contains(t, body, `"kind":"cidr"`)
	require.Contains(t, body, `"domain":"127.0.0.5/32"`)
}

func TestPatchingTheKindReparsesTheStoredValue(t *testing.T) {
	h := startHarness(t)

	status, _ := h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"games.example.com","kind":"exact","action":"allow"}`)
	require.Equal(t, http.StatusCreated, status)

	status, body := h.do(t, http.MethodPut, "/api/v1/rules/1", `{"kind":"cidr"}`)
	require.Equal(t, http.StatusBadRequest, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/rules", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"kind":"exact"`)
	require.Contains(t, body, `"domain":"games.example.com"`)
}

func TestPatchingTheKindReplacesTheStoredPayload(t *testing.T) {
	h := startHarness(t)

	status, _ := h.do(t, http.MethodPost, "/api/v1/rules",
		`{"domain":"games.example.com","kind":"exact","action":"block"}`)
	require.Equal(t, http.StatusCreated, status)

	status, body := h.do(t, http.MethodPut, "/api/v1/rules/1", `{"kind":"cidr","domain":"127.0.0.5/32"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"kind":"cidr"`)
	require.Contains(t, body, `"domain":"127.0.0.5/32"`)

	status, body = h.do(t, http.MethodGet, "/api/v1/rules", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"kind":"cidr"`)
	require.NotContains(t, body, "games.example.com")
}

func TestARuleIDMustBeANumber(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/rules/games", `{"action":"block"}`)

	require.Equal(t, http.StatusBadRequest, status, body)
}
