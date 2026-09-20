package runtime_test

import (
	"database/sql"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"aegis/internal/filter"
	"aegis/internal/runtime"
	"aegis/internal/store"
	"aegis/internal/upstream"
)

// deadUpstream is the row openStore seeds, so a Reload has a resolver that
// answers nothing. Nothing listens on the port, and a refused dial costs the
// test nothing because these tests never resolve through it.
const deadUpstream = "127.0.0.1:1"

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

func whoAnswered(t *testing.T, resp *mdns.Msg) string {
	t.Helper()
	a, ok := resp.Answer[0].(*mdns.A)
	require.True(t, ok, "expected an A answer, got %T", resp.Answer[0])
	return netip.AddrFrom4([4]byte(a.A)).String()
}

func resolveThrough(t *testing.T, sw *upstream.Switch, name string) *mdns.Msg {
	t.Helper()
	req := new(mdns.Msg).SetQuestion(name, mdns.TypeA)
	resp, err := sw.Resolve(t.Context(), req, "")
	require.NoError(t, err)
	return resp
}

func TestThePoolFollowsTheStoredUpstreams(t *testing.T) {
	ctx := t.Context()
	a := startStub(t, "203.0.113.10")
	b := startStub(t, "203.0.113.11")
	s := openStore(t)
	require.NoError(t, s.DeleteUpstream(ctx, deadUpstream))
	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "primary", URL: a.address, Enabled: true}))
	rt := runtime.New(s, nil, quietLogger())
	require.NoError(t, rt.Reload(ctx))

	resolver := rt.Upstreams()
	require.Equal(t, "203.0.113.10", whoAnswered(t, resolveThrough(t, resolver, "before.example.com.")))

	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "primary", URL: b.address, Enabled: true}))
	require.NoError(t, rt.Reload(ctx))

	require.Equal(t, "203.0.113.11", whoAnswered(t, resolveThrough(t, resolver, "after.example.com.")),
		"the same switch serves the pool the store now names")
	require.EqualValues(t, 1, a.attempts.Load())
	require.EqualValues(t, 1, b.attempts.Load())
}

func TestDecideNamesTheStoredRouteForTheQueriedDomain(t *testing.T) {
	ctx := t.Context()
	a := startStub(t, "203.0.113.10")
	b := startStub(t, "203.0.113.11")
	s := openStore(t)
	require.NoError(t, s.DeleteUpstream(ctx, deadUpstream))
	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "primary", URL: a.address, Enabled: true}))
	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "second", URL: b.address, Enabled: true}))
	_, err := s.SaveRoute(ctx, store.Route{Domain: "target.example", Upstream: "second"})
	require.NoError(t, err)
	rt := runtime.New(s, nil, quietLogger())
	require.NoError(t, rt.Reload(ctx))

	routed, err := filter.ParseDomain("x.target.example.")
	require.NoError(t, err)
	verdict := rt.Decide(routed, netip.Addr{})
	require.Equal(t, "second", verdict.Route)

	resp, err := rt.Upstreams().Resolve(ctx, new(mdns.Msg).SetQuestion(mdns.Fqdn(routed.String()), mdns.TypeA), verdict.Route)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.11", whoAnswered(t, resp))
	require.EqualValues(t, 0, a.attempts.Load(), "a routed query never touches the other resolver")

	plain, err := filter.ParseDomain("other.example.com")
	require.NoError(t, err)
	require.Equal(t, "", rt.Decide(plain, netip.Addr{}).Route)
}

func TestReloadRefusesAStoreWithoutAnEnabledUpstream(t *testing.T) {
	s := openStore(t)
	require.NoError(t, s.DeleteUpstream(t.Context(), deadUpstream))
	rt := runtime.New(s, nil, quietLogger())

	err := rt.Reload(t.Context())

	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one resolver is required")
	require.EqualError(t, func() error {
		_, err := rt.Upstreams().Resolve(t.Context(), new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA), "")
		return err
	}(), "upstream: no resolvers are loaded", "nothing was published")
}

func TestReloadNamesTheRowOfAStoredURLThatNoLongerParses(t *testing.T) {
	ctx := t.Context()
	path := t.TempDir() + "/aegis.db"
	s, err := store.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.SaveUpstream(ctx, store.Upstream{Name: "primary", URL: "9.9.9.9:53", Enabled: true}))
	rt := runtime.New(s, nil, quietLogger())
	require.NoError(t, rt.Reload(ctx))

	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = raw.ExecContext(ctx, "UPDATE upstreams SET url = 'ftp://9.9.9.9' WHERE name = 'primary'")
	require.NoError(t, err)

	reloadErr := rt.Reload(ctx)

	require.Error(t, reloadErr)
	require.Contains(t, reloadErr.Error(), `"primary"`)
	require.Contains(t, reloadErr.Error(), "unknown scheme")
}
