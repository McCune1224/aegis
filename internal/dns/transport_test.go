package dns_test

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/dns"
)

// startUpstream runs a real DNS server on one port for both transports, so the
// TCP retry has somewhere to land.
func startUpstream(t *testing.T, handle mdns.HandlerFunc) string {
	t.Helper()
	for range 10 {
		var listen net.ListenConfig
		pc, err := listen.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
		require.NoError(t, err)
		address := pc.LocalAddr().String()

		ln, err := listen.Listen(t.Context(), "tcp", address)
		if err != nil {
			_ = pc.Close()
			continue
		}

		udp := &mdns.Server{PacketConn: pc, Handler: handle}
		tcp := &mdns.Server{Listener: ln, Handler: handle}
		go func() { _ = udp.ActivateAndServe() }()
		go func() { _ = tcp.ActivateAndServe() }()
		t.Cleanup(func() {
			_ = udp.Shutdown()
			_ = tcp.Shutdown()
		})
		return address
	}
	t.Fatal("could not bind one port for both transports")
	return ""
}

func aRecord(name, address string) *mdns.A {
	return &mdns.A{
		Hdr: mdns.RR_Header{Name: name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 300},
		A:   netip.MustParseAddr(address).AsSlice(),
	}
}

func answerWith(address string) mdns.HandlerFunc {
	return func(w mdns.ResponseWriter, req *mdns.Msg) {
		resp := new(mdns.Msg)
		resp.SetReply(req)
		resp.Answer = append(resp.Answer, aRecord(req.Question[0].Name, address))
		_ = w.WriteMsg(resp)
	}
}

func exchange(t *testing.T, network, address string, req *mdns.Msg) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{Net: network, Timeout: 3 * time.Second}
	resp, _, err := client.Exchange(req, address)
	require.NoError(t, err, "network=%s address=%s", network, address)
	return resp
}

func TestForwarderResolvesOverUdp(t *testing.T) {
	upstream := startUpstream(t, answerWith("203.0.113.20"))

	got, err := dns.NewForwarder(upstream).Resolve(context.Background(), query("example.com.", mdns.TypeA))

	require.NoError(t, err)
	require.Len(t, got.Answer, 1)
	require.Equal(t, netip.MustParseAddr("203.0.113.20").AsSlice(), []byte(got.Answer[0].(*mdns.A).A))
}

func TestForwarderRetriesOverTcpWhenTheUdpAnswerIsTruncated(t *testing.T) {
	overTCP := "203.0.113.21"
	upstream := startUpstream(t, func(w mdns.ResponseWriter, req *mdns.Msg) {
		resp := new(mdns.Msg)
		resp.SetReply(req)
		_, isTCP := w.RemoteAddr().(*net.TCPAddr)
		if isTCP {
			resp.Answer = append(resp.Answer, aRecord(req.Question[0].Name, overTCP))
		} else {
			resp.Truncated = true
		}
		_ = w.WriteMsg(resp)
	})

	got, err := dns.NewForwarder(upstream).Resolve(context.Background(), query("big.example.com.", mdns.TypeA))

	require.NoError(t, err)
	require.False(t, got.Truncated)
	require.Len(t, got.Answer, 1)
	require.Equal(t, netip.MustParseAddr(overTCP).AsSlice(), []byte(got.Answer[0].(*mdns.A).A))
}

func TestForwarderHonoursACancelledContext(t *testing.T) {
	upstream := startUpstream(t, answerWith("203.0.113.22"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := dns.NewForwarder(upstream).Resolve(ctx, query("example.com.", mdns.TypeA))

	require.Error(t, err)
}

func TestServerAnswersOverUdpAndTcp(t *testing.T) {
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t, blockedAds()),
		Upstream: dns.NewForwarder(startUpstream(t, answerWith("203.0.113.23"))),
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)

	server, err := dns.Start(dns.ServerConfig{Handler: handler, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	udp := exchange(t, "udp", server.UDPAddr().String(), query("ads.example.com.", mdns.TypeA))
	require.Equal(t, mdns.RcodeNameError, udp.Rcode)

	tcp := exchange(t, "tcp", server.TCPAddr().String(), query("ads.example.com.", mdns.TypeA))
	require.Equal(t, mdns.RcodeNameError, tcp.Rcode)
}

func TestServerBlocksByRuleAndForwardsTheRestThroughARealUpstream(t *testing.T) {
	upstreamAddress := "203.0.113.30"
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t, blockedAds()),
		Upstream: dns.NewForwarder(startUpstream(t, answerWith(upstreamAddress))),
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)

	server, err := dns.Start(dns.ServerConfig{Handler: handler, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	address := server.UDPAddr().String()

	blocked := exchange(t, "udp", address, query("ads.example.com.", mdns.TypeA))
	require.Equal(t, mdns.RcodeNameError, blocked.Rcode)
	require.Empty(t, blocked.Answer)

	allowed := exchange(t, "udp", address, query("example.com.", mdns.TypeA))
	require.Equal(t, mdns.RcodeSuccess, allowed.Rcode)
	require.Len(t, allowed.Answer, 1)
	require.Equal(t, netip.MustParseAddr(upstreamAddress).AsSlice(), []byte(allowed.Answer[0].(*mdns.A).A))
}

func TestServerStopsAnsweringAfterShutdown(t *testing.T) {
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t),
		Upstream: dns.NewForwarder(startUpstream(t, answerWith("203.0.113.24"))),
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)

	server, err := dns.Start(dns.ServerConfig{Handler: handler, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	address := server.UDPAddr().String()

	require.NoError(t, server.Shutdown(context.Background()))

	client := &mdns.Client{Net: "udp", Timeout: time.Second}
	_, _, err = client.Exchange(query("example.com.", mdns.TypeA), address)
	require.Error(t, err)
}

func TestStartRejectsAnIncompleteConfig(t *testing.T) {
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t),
		Upstream: dns.NewForwarder("127.0.0.1:53"),
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)

	_, err = dns.Start(dns.ServerConfig{Address: "127.0.0.1:0"})
	require.Error(t, err)

	_, err = dns.Start(dns.ServerConfig{Handler: handler})
	require.Error(t, err)
}

func TestStartReportsThePortItBound(t *testing.T) {
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t),
		Upstream: dns.NewForwarder("127.0.0.1:53"),
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)

	server, err := dns.Start(dns.ServerConfig{Handler: handler, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	udpPort := server.UDPAddr().(*net.UDPAddr).Port
	require.NotZero(t, udpPort)
	require.Equal(t, fmt.Sprintf("127.0.0.1:%d", udpPort), server.UDPAddr().String())
}
