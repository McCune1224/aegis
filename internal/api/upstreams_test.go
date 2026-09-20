package api_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// stub is a real DNS server on an ephemeral port whose every answer carries
// one marker address, so the test reads who served the query off the wire.
type stub struct {
	address  string
	packet   net.PacketConn
	server   *mdns.Server
	attempts atomic.Int32
}

func startStub(t *testing.T, answer string) *stub {
	t.Helper()
	var listen net.ListenConfig
	packet, err := listen.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &stub{address: packet.LocalAddr().String(), packet: packet}
	s.server = &mdns.Server{
		PacketConn: packet,
		Handler: mdns.HandlerFunc(func(w mdns.ResponseWriter, req *mdns.Msg) {
			s.attempts.Add(1)
			resp := new(mdns.Msg)
			resp.SetReply(req)
			resp.Answer = append(resp.Answer, &mdns.A{
				Hdr: mdns.RR_Header{Name: req.Question[0].Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 60},
				A:   netip.MustParseAddr(answer).AsSlice(),
			})
			_ = w.WriteMsg(resp)
		}),
	}
	go func() { _ = s.server.ActivateAndServe() }()
	t.Cleanup(func() {
		_ = s.server.Shutdown()
		_ = s.packet.Close()
	})
	return s
}

func answerAddress(t *testing.T, resp *mdns.Msg) string {
	t.Helper()
	a, ok := resp.Answer[0].(*mdns.A)
	require.True(t, ok, "expected an A answer, got %T", resp.Answer[0])
	return netip.AddrFrom4([4]byte(a.A)).String()
}

func TestAnUpstreamEditTakesEffectWithoutARestart(t *testing.T) {
	h := startHarness(t)
	a := startStub(t, "203.0.113.10")
	b := startStub(t, "203.0.113.11")

	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+a.address+`","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)

	got := askName(t, h.dnsAddress, "before.example.com.")
	require.Equal(t, "203.0.113.10", answerAddress(t, got))
	require.EqualValues(t, 1, a.attempts.Load())

	status, body = h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+b.address+`","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)

	got = askName(t, h.dnsAddress, "after.example.com.")
	require.Equal(t, "203.0.113.11", answerAddress(t, got), "the edit is live on the same server")
	require.EqualValues(t, 1, a.attempts.Load())

	status, body = h.do(t, http.MethodGet, "/api/v1/upstreams", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"primary"`)
	require.Contains(t, body, `"url":"udp://`+b.address+`"`)

	// Disabling a row takes it out of the pool on the next query.
	status, body = h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+b.address+`","enabled":false}`)
	require.Equal(t, http.StatusOK, status, body)
	got = askName(t, h.dnsAddress, "disabled.example.com.")
	require.Equal(t, mdns.RcodeServerFailure, got.Rcode, "no enabled resolver is left to ask")
}

func TestTheLastEnabledUpstreamCannotBeRemovedOrDisabled(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodDelete, "/api/v1/upstreams/"+deadUpstream, "")
	require.Equal(t, http.StatusBadRequest, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/upstreams/"+deadUpstream, `{"url":"`+deadUpstream+`","enabled":false}`)
	require.Equal(t, http.StatusBadRequest, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/upstreams", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"name":"`+deadUpstream+`"`)
	require.Contains(t, body, `"enabled":true`, "the refused write changed nothing")
}

func TestAnUpstreamRowDeletionKeepsTheRemainingResolvers(t *testing.T) {
	h := startHarness(t)
	a := startStub(t, "203.0.113.10")

	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+a.address+`","enabled":true,"backup":true}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/upstreams/"+deadUpstream, "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/upstreams", "")
	require.Equal(t, http.StatusOK, status, body)
	require.NotContains(t, body, deadUpstream)
	require.Contains(t, body, `"backup":true`)

	got := askName(t, h.dnsAddress, "kept.example.com.")
	require.Equal(t, "203.0.113.10", answerAddress(t, got), "the remaining resolver serves")
}

type upstreamRowJSON struct {
	Name      string  `json:"name"`
	URL       string  `json:"url"`
	Enabled   bool    `json:"enabled"`
	LatencyMS float64 `json:"latency_ms"`
	Failures  int     `json:"failures"`
	Down      bool    `json:"down"`
}

func TestUpstreamRowsCarryLiveHealth(t *testing.T) {
	h := startHarness(t)
	live := startStub(t, "203.0.113.10")

	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+live.address+`","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)

	askName(t, h.dnsAddress, "health-one.example.com.")
	askName(t, h.dnsAddress, "health-two.example.com.")

	status, body = h.do(t, http.MethodGet, "/api/v1/upstreams", "")
	require.Equal(t, http.StatusOK, status, body)
	var listed []upstreamRowJSON
	require.NoError(t, json.Unmarshal([]byte(body), &listed))
	require.Len(t, listed, 2)
	rows := map[string]upstreamRowJSON{}
	for _, row := range listed {
		rows[row.Name] = row
	}
	require.Greater(t, rows["primary"].LatencyMS, 0.0, "the live resolver has a measured latency")
	require.Equal(t, 0, rows["primary"].Failures)
	require.False(t, rows["primary"].Down)
	require.Equal(t, 0.0, rows[deadUpstream].LatencyMS)
	require.Equal(t, 2, rows[deadUpstream].Failures, "both queries probed the dead resolver first")
	require.True(t, rows[deadUpstream].Down, "the failures crossed the threshold")

	status, body = h.do(t, http.MethodPut, "/api/v1/upstreams/spare", `{"url":"9.9.9.9","enabled":false}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/upstreams", "")
	require.Equal(t, http.StatusOK, status, body)
	listed = nil
	require.NoError(t, json.Unmarshal([]byte(body), &listed))
	rows = map[string]upstreamRowJSON{}
	for _, row := range listed {
		rows[row.Name] = row
	}
	require.Equal(t, 0.0, rows["spare"].LatencyMS, "a row outside the running pool reads zero")
	require.Equal(t, 0, rows["spare"].Failures)
	require.False(t, rows["spare"].Down)
}

func TestUpstreamInputIsRejectedWithFourHundred(t *testing.T) {
	h := startHarness(t)

	status, _ := h.do(t, http.MethodPut, "/api/v1/upstreams/bad", `{"url":"ftp://9.9.9.9","enabled":true}`)
	require.Equal(t, http.StatusBadRequest, status)

	status, _ = h.do(t, http.MethodPut, "/api/v1/upstreams/bad", `{"enabled":true}`)
	require.Equal(t, http.StatusBadRequest, status)

	status, _ = h.do(t, http.MethodPut, "/api/v1/upstreams/bad", `{"url":"9.9.9.9:53"}`)
	require.Equal(t, http.StatusBadRequest, status)

	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/bad", `{`)
	require.Equal(t, http.StatusBadRequest, status, body)
}
