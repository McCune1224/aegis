package upstream_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/upstream"
)

// stub is a real DNS server on an ephemeral port whose lifecycle the test
// controls, so a test can kill and revive it the way a resolver dies. Its
// handler answers every question with one marker address, so the test reads
// who spoke straight off the answer.
type stub struct {
	t        *testing.T
	handler  mdns.HandlerFunc
	address  string
	packet   net.PacketConn
	listener net.Listener
	servers  []*mdns.Server
	attempts atomic.Int32
}

func startStub(t *testing.T, handler mdns.HandlerFunc) *stub {
	s := &stub{t: t, handler: handler}
	s.serve()
	t.Cleanup(s.stop)
	return s
}

func (s *stub) serve() {
	pc, ln := s.bind()
	s.address = pc.LocalAddr().String()

	counting := mdns.HandlerFunc(func(w mdns.ResponseWriter, req *mdns.Msg) {
		s.attempts.Add(1)
		s.handler(w, req)
	})
	udp := &mdns.Server{PacketConn: pc, Handler: counting}
	tcp := &mdns.Server{Listener: ln, Handler: counting}
	go func() { _ = udp.ActivateAndServe() }()
	go func() { _ = tcp.ActivateAndServe() }()
	s.packet, s.listener, s.servers = pc, ln, []*mdns.Server{udp, tcp}
}

// bind opens the UDP socket and the TCP listener on one address. The kernel
// picks a port for the UDP socket, and nothing holds that port while the TCP
// listener binds, so a loaded machine can hand it to someone else first. Losing
// that race is retried with a fresh port rather than reported, because it says
// nothing about the code under test.
func (s *stub) bind() (net.PacketConn, net.Listener) {
	var listen net.ListenConfig
	for attempt := range 10 {
		desired := s.address
		if desired == "" {
			desired = "127.0.0.1:0"
		}
		pc, err := listen.ListenPacket(context.Background(), "udp", desired)
		require.NoError(s.t, err)

		ln, err := listen.Listen(context.Background(), "tcp", pc.LocalAddr().String())
		if err == nil {
			return pc, ln
		}
		_ = pc.Close()
		if desired != "127.0.0.1:0" || attempt == 9 {
			require.NoError(s.t, err)
		}
	}
	panic("unreachable")
}

func (s *stub) stop() {
	for _, server := range s.servers {
		_ = server.Shutdown()
	}
	s.servers = nil
	if s.packet != nil {
		_ = s.packet.Close()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

// revive restarts the stub on the address it had, so a pool that marked it
// down can reach it again once its backoff elapses.
func (s *stub) revive() {
	s.stop()
	s.serve()
}

// clock is a wall clock the test can move, so a backoff can elapse without
// the test waiting it out.
type clock struct {
	offset atomic.Int64
}

func (c *clock) Now() time.Time { return time.Now().Add(time.Duration(c.offset.Load())) }

func (c *clock) Advance(d time.Duration) { c.offset.Add(int64(d)) }

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

// drop accepts the query and answers nothing, so every attempt against it
// fails. The handler counts the attempt, which is how a test tells a skipped
// resolver from a retried one.
func drop() mdns.HandlerFunc {
	return func(_ mdns.ResponseWriter, _ *mdns.Msg) {}
}

func newPool(t *testing.T, change func(*upstream.Config), raw ...string) *upstream.Pool {
	t.Helper()
	cfg := upstream.Config{}
	for _, spec := range raw {
		parsed, err := upstream.Parse(spec)
		require.NoError(t, err)
		cfg.Specs = append(cfg.Specs, parsed)
	}
	if change != nil {
		change(&cfg)
	}
	pool, err := upstream.New(cfg)
	require.NoError(t, err)
	return pool
}

func resolve(t *testing.T, pool *upstream.Pool) *mdns.Msg {
	t.Helper()
	req := new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA)
	resp, err := pool.Resolve(context.Background(), req)
	require.NoError(t, err)
	return resp
}

// tryResolve asks the pool and hands back the error, for queries that are
// expected to fail.
func tryResolve(t *testing.T, pool *upstream.Pool) error {
	t.Helper()
	req := new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA)
	_, err := pool.Resolve(context.Background(), req)
	return err
}

func whoAnswered(t *testing.T, resp *mdns.Msg) string {
	t.Helper()
	a, ok := resp.Answer[0].(*mdns.A)
	require.True(t, ok, "expected an A answer, got %T", resp.Answer[0])
	return netip.AddrFrom4([4]byte(a.A)).String()
}

func TestResolveUpstreamAsksOnlyTheNamedResolver(t *testing.T) {
	a := startStub(t, answerWith("203.0.113.10"))
	b := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, nil, a.address, b.address)

	req := new(mdns.Msg).SetQuestion("internal.example.", mdns.TypeA)
	resp, err := pool.ResolveUpstream(context.Background(), req, b.address)

	require.NoError(t, err)
	require.Equal(t, "203.0.113.11", whoAnswered(t, resp))
	require.EqualValues(t, 0, a.attempts.Load(), "a routed query never touches another resolver")
	require.EqualValues(t, 1, b.attempts.Load())
}

func TestARoutedQueryFailsWithItsOwnUpstreamInsteadOfFailingOver(t *testing.T) {
	a := startStub(t, answerWith("203.0.113.10"))
	dead := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, nil, a.address, dead.address)
	dead.stop()

	req := new(mdns.Msg).SetQuestion("internal.example.", mdns.TypeA)
	_, err := pool.ResolveUpstream(context.Background(), req, dead.address)

	require.Error(t, err)
	require.Contains(t, err.Error(), dead.address)
	require.EqualValues(t, 0, a.attempts.Load(),
		"a split-horizon name must not leak to the next resolver")
}

func TestResolveUpstreamNamesAnUnknownResolver(t *testing.T) {
	a := startStub(t, answerWith("203.0.113.10"))
	pool := newPool(t, nil, a.address)

	_, err := pool.ResolveUpstream(context.Background(), new(mdns.Msg).SetQuestion("internal.example.", mdns.TypeA), "ghost")

	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
}

func TestABackupResolverStaysIdleWhileAPrimaryAnswers(t *testing.T) {
	primary := startStub(t, answerWith("203.0.113.10"))
	backup := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, func(cfg *upstream.Config) { cfg.Specs[1].Backup = true }, primary.address, backup.address)

	for range 2 {
		require.Equal(t, "203.0.113.10", whoAnswered(t, resolve(t, pool)))
	}

	require.EqualValues(t, 2, primary.attempts.Load())
	require.EqualValues(t, 0, backup.attempts.Load(),
		"the backup never shares the rotation while a primary is eligible")
}

func TestABackupResolverTakesOverOnlyWhileEveryPrimaryIsDown(t *testing.T) {
	tick := &clock{}
	primary := startStub(t, answerWith("203.0.113.10"))
	backup := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, func(cfg *upstream.Config) {
		cfg.Attempt = 50 * time.Millisecond
		cfg.Now = tick.Now
		cfg.Specs[1].Backup = true
	}, primary.address, backup.address)
	require.Equal(t, "203.0.113.10", whoAnswered(t, resolve(t, pool)))

	primary.stop()
	require.Error(t, tryResolve(t, pool))
	require.Error(t, tryResolve(t, pool), "a second failure marks the primary down")
	require.Equal(t, "203.0.113.11", whoAnswered(t, resolve(t, pool)),
		"the backup answers once no primary is eligible")
	backupSeen := backup.attempts.Load()

	primary.revive()
	tick.Advance(11 * time.Second)
	require.Equal(t, "203.0.113.10", whoAnswered(t, resolve(t, pool)),
		"an eligible primary comes back and the backup sits out again")
	require.EqualValues(t, backupSeen, backup.attempts.Load())
}

func TestPoolStatsSnapshotsEveryResolverInConfiguredOrder(t *testing.T) {
	tick := &clock{}
	silent := startStub(t, drop())
	live := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, func(cfg *upstream.Config) {
		cfg.Attempt = 50 * time.Millisecond
		cfg.Now = tick.Now
	}, silent.address, live.address)

	stats := pool.Stats()
	require.Len(t, stats, 2)
	require.Equal(t, silent.address, stats[0].Name)
	require.Equal(t, "udp://"+silent.address, stats[0].URL)
	require.Zero(t, stats[0].EWMA, "an unqueried resolver has no sample")
	require.Equal(t, 0, stats[0].Failures)
	require.False(t, stats[0].Down)
	require.Equal(t, live.address, stats[1].Name)

	resolve(t, pool)
	resolve(t, pool)

	stats = pool.Stats()
	require.Positive(t, stats[1].EWMA, "a successful exchange measures latency")
	require.Equal(t, 0, stats[1].Failures)
	require.False(t, stats[1].Down)
	require.Equal(t, 2, stats[0].Failures)
	require.True(t, stats[0].Down, "the failure threshold marks the resolver down")

	tick.Advance(11 * time.Second)
	require.False(t, pool.Stats()[0].Down, "the backoff expiry puts the resolver back in play")
}

func TestParseReadsEveryScheme(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		scheme string
		host   string
		port   string
	}{
		{"bare host and port", "9.9.9.9:53", "udp", "9.9.9.9", "53"},
		{"bare host without a port", "9.9.9.9", "udp", "9.9.9.9", "53"},
		{"bare ipv6 with brackets", "[::1]:53", "udp", "::1", "53"},
		{"udp with an explicit port", "udp://1.1.1.1:5353", "udp", "1.1.1.1", "5353"},
		{"tcp", "tcp://1.1.1.1:53", "tcp", "1.1.1.1", "53"},
		{"tls without a port", "tls://dns.quad9.net", "tls", "dns.quad9.net", "853"},
		{"https with a path", "https://dns.quad9.net/dns-query", "https", "dns.quad9.net", "443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := upstream.Parse(tc.raw)

			require.NoError(t, err)
			require.Equal(t, tc.raw, spec.Name)
			require.Equal(t, tc.scheme, spec.URL.Scheme)
			require.Equal(t, tc.host, spec.URL.Hostname())
			require.Equal(t, tc.port, spec.URL.Port())
		})
	}
}

func TestParseRejectsUnusableForms(t *testing.T) {
	for _, raw := range []string{
		"",
		"   ",
		"ftp://9.9.9.9",
		"http://9.9.9.9",
		"https://",
		"udp://",
		"udp://9.9.9.9/dns-query",
		"tls://9.9.9.9?backoff=1",
		"udp://9.9.9.9:notaport",
		"udp://9.9.9.9:0",
		"udp://user:pass@9.9.9.9",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := upstream.Parse(raw)
			require.Error(t, err)
		})
	}
}

func TestPoolRefusesToStartWithoutResolvers(t *testing.T) {
	_, err := upstream.New(upstream.Config{})
	require.Error(t, err)
}

func TestPoolAsksTheFirstConfiguredResolverWhileItIsHealthy(t *testing.T) {
	a := startStub(t, answerWith("203.0.113.10"))
	b := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, nil, a.address, b.address)

	require.Equal(t, "203.0.113.10", whoAnswered(t, resolve(t, pool)))

	require.EqualValues(t, 1, a.attempts.Load())
	require.EqualValues(t, 0, b.attempts.Load())
}

func TestPoolFailsOverWhenAnUpstreamDies(t *testing.T) {
	a := startStub(t, answerWith("203.0.113.10"))
	b := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, nil, a.address, b.address)
	require.Equal(t, "203.0.113.10", whoAnswered(t, resolve(t, pool)))

	a.stop()

	require.Equal(t, "203.0.113.11", whoAnswered(t, resolve(t, pool)),
		"the query after the death is answered by the surviving resolver")
	require.EqualValues(t, 1, b.attempts.Load())

	require.Equal(t, "203.0.113.11", whoAnswered(t, resolve(t, pool)),
		"the surviving resolver keeps serving")
	require.EqualValues(t, 2, b.attempts.Load())
}

func TestPoolMarksAnUpstreamDownAndProbesItAgainAfterTheBackoff(t *testing.T) {
	tick := &clock{}
	silent := startStub(t, drop())
	healthy := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, func(cfg *upstream.Config) {
		cfg.Attempt = 50 * time.Millisecond
		cfg.Now = tick.Now
	}, silent.address, healthy.address)

	// Two failed attempts mark the resolver down. The third query must skip
	// it entirely, and the probe after the backoff must touch it once.
	resolve(t, pool)
	resolve(t, pool)
	require.EqualValues(t, 2, silent.attempts.Load())
	require.EqualValues(t, 2, healthy.attempts.Load())

	resolve(t, pool)
	require.EqualValues(t, 2, silent.attempts.Load(), "a down resolver is skipped, not retried")
	require.EqualValues(t, 3, healthy.attempts.Load())

	tick.Advance(11 * time.Second)
	resolve(t, pool)
	require.EqualValues(t, 3, silent.attempts.Load(), "the backoff expiry puts the resolver back in play")
	require.EqualValues(t, 4, healthy.attempts.Load())
}

func TestPoolTriesAResolverAgainAfterItComesBack(t *testing.T) {
	tick := &clock{}
	a := startStub(t, answerWith("203.0.113.10"))
	pool := newPool(t, func(cfg *upstream.Config) { cfg.Now = tick.Now }, a.address)
	require.Equal(t, "203.0.113.10", whoAnswered(t, resolve(t, pool)))

	a.stop()
	req := new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA)
	_, err := pool.Resolve(context.Background(), req)
	require.Error(t, err)
	_, err = pool.Resolve(context.Background(), req)
	require.Error(t, err)

	a.revive()
	tick.Advance(11 * time.Second)

	resp, err := pool.Resolve(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.10", whoAnswered(t, resp),
		"a revived resolver answers again once its backoff elapses")
}

func TestPoolTriesOneResolverPerQueryWhenEveryResolverIsDown(t *testing.T) {
	tick := &clock{}
	a := startStub(t, drop())
	b := startStub(t, drop())
	pool := newPool(t, func(cfg *upstream.Config) {
		cfg.Attempt = 40 * time.Millisecond
		cfg.Now = tick.Now
	}, a.address, b.address)

	// While both are merely failing, one query walks both.
	require.Error(t, tryResolve(t, pool))
	require.Error(t, tryResolve(t, pool))
	require.EqualValues(t, 2, a.attempts.Load())
	require.EqualValues(t, 2, b.attempts.Load())

	// Once both are down, a query spends its budget on the single best peer
	// rather than serialising a timeout per configured resolver.
	require.Error(t, tryResolve(t, pool))
	require.EqualValues(t, 3, a.attempts.Load())
	require.EqualValues(t, 2, b.attempts.Load())
}

func TestPoolPrefersTheFasterResolver(t *testing.T) {
	slow := startStub(t, func(w mdns.ResponseWriter, req *mdns.Msg) {
		time.Sleep(250 * time.Millisecond)
		answerWith("203.0.113.10")(w, req)
	})
	fast := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, nil, slow.address, fast.address)

	require.Equal(t, "203.0.113.10", whoAnswered(t, resolve(t, pool)), "an unmeasured resolver is probed once")
	require.Equal(t, "203.0.113.11", whoAnswered(t, resolve(t, pool)), "the unmeasured peer ranks ahead after a slow first sample")
	require.Equal(t, "203.0.113.11", whoAnswered(t, resolve(t, pool)))
	require.Equal(t, "203.0.113.11", whoAnswered(t, resolve(t, pool)))

	require.EqualValues(t, 1, slow.attempts.Load())
	require.EqualValues(t, 3, fast.attempts.Load())
}

func TestPoolBoundsAHangingResolver(t *testing.T) {
	hanging := startStub(t, func(w mdns.ResponseWriter, req *mdns.Msg) {
		time.Sleep(300 * time.Millisecond)
		answerWith("203.0.113.10")(w, req)
	})
	quick := startStub(t, answerWith("203.0.113.11"))
	pool := newPool(t, func(cfg *upstream.Config) { cfg.Attempt = 50 * time.Millisecond }, hanging.address, quick.address)

	require.Equal(t, "203.0.113.11", whoAnswered(t, resolve(t, pool)),
		"an attempt past its budget fails over instead of waiting")
	require.EqualValues(t, 1, hanging.attempts.Load())
}

func TestPoolPassesSERVFAILThroughWithoutFailingOver(t *testing.T) {
	servfail := startStub(t, func(w mdns.ResponseWriter, req *mdns.Msg) {
		resp := new(mdns.Msg)
		resp.SetRcode(req, mdns.RcodeServerFailure)
		_ = w.WriteMsg(resp)
	})
	slow := startStub(t, func(w mdns.ResponseWriter, req *mdns.Msg) {
		time.Sleep(100 * time.Millisecond)
		answerWith("203.0.113.11")(w, req)
	})
	pool := newPool(t, nil, servfail.address, slow.address)

	resp := resolve(t, pool)

	require.Equal(t, mdns.RcodeServerFailure, resp.Rcode)
	require.EqualValues(t, 1, servfail.attempts.Load())
	require.EqualValues(t, 0, slow.attempts.Load(), "an answer from an up resolver is not retried elsewhere")

	// The fast SERVFAIL resolver keeps ranking first: if a SERVFAIL response
	// counted as a failure, two more queries would mark it down and the slow
	// peer would take over.
	for range 4 {
		resolve(t, pool)
	}
	require.EqualValues(t, 4, servfail.attempts.Load(), "a resolver that answers SERVFAIL stays in play")
	require.EqualValues(t, 1, slow.attempts.Load(), "only the initial probe of the unmeasured peer happens")
}

func TestPoolHonoursACancelledContext(t *testing.T) {
	a := startStub(t, answerWith("203.0.113.10"))
	pool := newPool(t, nil, a.address)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := pool.Resolve(ctx, new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA))

	require.ErrorIs(t, err, context.Canceled)
}

func TestPoolRetriesOverTcpWhenTheUdpAnswerIsTruncated(t *testing.T) {
	overTCP := "203.0.113.21"
	one := startStub(t, func(w mdns.ResponseWriter, req *mdns.Msg) {
		resp := new(mdns.Msg)
		resp.SetReply(req)
		if _, isTCP := w.RemoteAddr().(*net.TCPAddr); isTCP {
			resp.Answer = append(resp.Answer, aRecord(req.Question[0].Name, overTCP))
		} else {
			resp.Truncated = true
		}
		_ = w.WriteMsg(resp)
	})
	pool := newPool(t, nil, one.address)

	got := resolve(t, pool)

	require.False(t, got.Truncated)
	require.Equal(t, overTCP, whoAnswered(t, got))
}

func TestPoolExchangesOverTLS(t *testing.T) {
	cert := selfSignedCertificate(t)
	roots := x509.NewCertPool()
	roots.AddCert(cert.Leaf)
	address := startTLSStub(t, cert, answerWith("203.0.113.31"))

	pool := newPool(t, func(cfg *upstream.Config) {
		cfg.TLS = &tls.Config{RootCAs: roots}
	}, "tls://"+address)

	require.Equal(t, "203.0.113.31", whoAnswered(t, resolve(t, pool)))
}

// seen records what the DoH server received, because the handler runs on its
// own goroutine and the race detector cannot see the round trip.
type seen struct {
	mu          sync.Mutex
	method      string
	contentType string
	accept      string
}

func (s *seen) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.method, s.contentType, s.accept = r.Method, r.Header.Get("Content-Type"), r.Header.Get("Accept")
}

func (s *seen) snapshot() (string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.method, s.contentType, s.accept
}

func TestPoolPostsDNSMessagesOverHTTPS(t *testing.T) {
	var seen seen
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.record(r)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		req := new(mdns.Msg)
		if err := req.Unpack(body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := new(mdns.Msg)
		resp.SetReply(req)
		resp.Answer = append(resp.Answer, aRecord(req.Question[0].Name, "203.0.113.41"))
		wire, err := resp.Pack()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(wire)
	}))
	t.Cleanup(server.Close)

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	pool := newPool(t, func(cfg *upstream.Config) {
		cfg.TLS = &tls.Config{RootCAs: roots}
	}, server.URL)

	require.Equal(t, "203.0.113.41", whoAnswered(t, resolve(t, pool)))

	method, contentType, accept := seen.snapshot()
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "application/dns-message", contentType)
	require.Equal(t, "application/dns-message", accept)
}

func startTLSStub(t *testing.T, cert tls.Certificate, handler mdns.HandlerFunc) string {
	t.Helper()
	var listen net.ListenConfig
	ln, err := listen.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	tlsLn := tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}})
	server := &mdns.Server{Listener: tlsLn, Handler: handler}
	go func() { _ = server.ActivateAndServe() }()
	t.Cleanup(func() { _ = server.Shutdown() })
	return ln.Addr().String()
}

func selfSignedCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "aegis test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}
