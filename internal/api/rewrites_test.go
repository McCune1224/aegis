package api_test

import (
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// putRewrite stores one rewrite through the API and fails the test on any
// non-200 answer.
func putRewrite(t *testing.T, h *harness, pattern, target string) {
	t.Helper()
	status, body := h.do(t, "PUT", "/api/v1/rewrites/"+url.PathEscape(pattern),
		`{"target":"`+target+`"}`)
	require.Equal(t, 200, status, body)
}

// askName resolves any name through the harness's DNS server, unlike queryFor
// and queryFrom, which each ask one fixed name.
func askName(t *testing.T, address, name string) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{Net: "udp", Timeout: 2 * time.Second}
	resp, _, err := client.Exchange(new(mdns.Msg).SetQuestion(name, mdns.TypeA), address)
	require.NoError(t, err)
	return resp
}

func TestARewriteAnswersARealQuery(t *testing.T) {
	h := startHarness(t)

	putRewrite(t, h, "nas.local", "192.0.2.44")

	status, body := h.do(t, http.MethodGet, "/api/v1/rewrites", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"pattern":"nas.local"`)
	require.Contains(t, body, `"target":"192.0.2.44"`)

	got := askName(t, h.dnsAddress, "nas.local.")
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 1)
	require.Equal(t, netip.MustParseAddr("192.0.2.44").AsSlice(), []byte(got.Answer[0].(*mdns.A).A))

	// The reverse direction answers from the same rewrite.
	ptr := askPTR(t, h.dnsAddress, "44.2.0.192.in-addr.arpa.")
	require.Equal(t, mdns.RcodeSuccess, ptr.Rcode)
	require.Equal(t, "nas.local.", ptr.Answer[0].(*mdns.PTR).Ptr)

	// Deleting the rewrite returns the name to the resolver, which the
	// harness does not run, so the query stops succeeding.
	status, body = h.do(t, http.MethodDelete, "/api/v1/rewrites/"+url.PathEscape("nas.local"), "")
	require.Equal(t, http.StatusNoContent, status, body)
	got = askName(t, h.dnsAddress, "nas.local.")
	require.Equal(t, mdns.RcodeServerFailure, got.Rcode)
}

func TestAWildcardRewriteAnswersSubdomainsThroughTheAPI(t *testing.T) {
	h := startHarness(t)

	putRewrite(t, h, "*.nas.local", "192.0.2.45")

	got := askName(t, h.dnsAddress, "cam.nas.local.")
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 1)
	require.Equal(t, netip.MustParseAddr("192.0.2.45").AsSlice(), []byte(got.Answer[0].(*mdns.A).A))
}

func TestRewriteInputIsRejectedWithFourHundred(t *testing.T) {
	h := startHarness(t)

	status, _ := h.do(t, "PUT", "/api/v1/rewrites/nas.local", `{"target":"not a target"}`)
	require.Equal(t, http.StatusBadRequest, status)

	status, _ = h.do(t, "PUT", "/api/v1/rewrites/nas.local", `{}`)
	require.Equal(t, http.StatusBadRequest, status)

	status, body := h.do(t, "PUT", "/api/v1/rewrites/", `{"target":"192.0.2.44"}`)
	require.Equal(t, http.StatusBadRequest, status, body)
}

func askPTR(t *testing.T, address, name string) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{Net: "udp", Timeout: 2 * time.Second}
	resp, _, err := client.Exchange(new(mdns.Msg).SetQuestion(name, mdns.TypePTR), address)
	require.NoError(t, err)
	return resp
}
