package dns_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/dns"
)

// pemBlock encodes DER bytes as one PEM block.
func pemBlock(kind string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der})
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func readAll(t *testing.T, body io.Reader) []byte {
	t.Helper()
	payload, err := io.ReadAll(body)
	require.NoError(t, err)
	return payload
}

func doRequest(t *testing.T, client *http.Client, method, url, contentType string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

// pinning generates a self-signed certificate for localhost, the private key
// as PEM, and the pool a client pins it with.
func pinning(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	certPEM := pemBlock("CERTIFICATE", der)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPEM := pemBlock("EC PRIVATE KEY", keyDER)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return pair, pool
}

// clientIdentity watches the addresses the handler serves. Every transport
// must deliver the same client address, or per-client policy breaks on the
// encrypted listeners.
type clientIdentity struct {
	mu     sync.Mutex
	seen   []netip.Addr
	answer bool
}

func (c *clientIdentity) Allow(address netip.Addr) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, address)
	return c.answer
}

func (c *clientIdentity) asked() []netip.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]netip.Addr(nil), c.seen...)
}

func tlsServerFor(t *testing.T, upstream dns.Resolver, limiter dns.RateLimiter, pair tls.Certificate) *dns.Server {
	t.Helper()
	handler, err := dns.NewHandler(dns.Config{
		Decider:  deciderFor(t, defaultPolicy),
		Upstream: upstream,
		Limiter:  limiter,
	})
	require.NoError(t, err)
	server, err := dns.Start(dns.ServerConfig{
		Handler:    handler,
		Address:    "127.0.0.1:0",
		DoTAddress: "127.0.0.1:0",
		DoHAddress: "127.0.0.1:0",
		TLS:        &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12},
		Logger:     discardLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })
	return server
}

func dotClient(t *testing.T, pool *x509.CertPool) *mdns.Client {
	t.Helper()
	return &mdns.Client{
		Net:       "tcp-tls",
		TLSConfig: &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS12},
		Timeout:   2 * time.Second,
		UDPSize:   1232,
	}
}

func dohClient(t *testing.T, pool *x509.CertPool) *http.Client {
	t.Helper()
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS12},
	}}
}

func TestStartServesDoTThroughThePinnedCertificate(t *testing.T) {
	pair, pool := pinning(t)
	upstream := resolverFunc(func(_ context.Context, req *mdns.Msg, _ string) (*mdns.Msg, error) {
		return upstreamA(req, "203.0.113.7"), nil
	})
	identity := &clientIdentity{answer: true}
	server := tlsServerFor(t, upstream, identity, pair)

	plain, _, err := new(mdns.Client{Net: "udp", Timeout: 2 * time.Second}).
		Exchange(query("example.net.", mdns.TypeA), server.UDPAddr().String())
	require.NoError(t, err)

	tlsQuery, _, err := dotClient(t, pool).
		Exchange(query("example.net.", mdns.TypeA), server.DoTAddr().String())
	require.NoError(t, err, "the TLS handshake must succeed against the pinned certificate")
	require.Equal(t, plain.Answer, tlsQuery.Answer, "the encrypted listener gives the plaintext answer")

	require.NotEmpty(t, identity.asked(), "the client must reach the handler identified")
	require.Equal(t, netip.MustParseAddr("127.0.0.1"), identity.asked()[len(identity.asked())-1].Unmap())
}

func TestStartServesDoHThroughThePinnedCertificate(t *testing.T) {
	pair, pool := pinning(t)
	upstream := resolverFunc(func(_ context.Context, req *mdns.Msg, _ string) (*mdns.Msg, error) {
		return upstreamA(req, "203.0.113.8"), nil
	})
	identity := &clientIdentity{answer: true}
	server := tlsServerFor(t, upstream, identity, pair)
	url := "https://" + server.DoHAddr().String() + "/dns"
	client := dohClient(t, pool)

	wire, err := query("example.net.", mdns.TypeA).Pack()
	require.NoError(t, err)

	post := doRequest(t, client, http.MethodPost, url, "application/dns-message", wire)
	defer func() { _ = post.Body.Close() }()
	require.Equal(t, http.StatusOK, post.StatusCode)
	require.Equal(t, "application/dns-message", post.Header.Get("Content-Type"))
	payload := readAll(t, post.Body)
	dohAnswer := new(mdns.Msg)
	require.NoError(t, dohAnswer.Unpack(payload))
	require.Len(t, dohAnswer.Answer, 1)
	a, ok := dohAnswer.Answer[0].(*mdns.A)
	require.True(t, ok)
	require.Equal(t, netip.MustParseAddr("203.0.113.8").AsSlice(), []byte(a.A),
		"DoH POST answers what plaintext answers")
	require.NotEmpty(t, post.Header.Get("Cache-Control"), "the response states how long it may be cached")

	get := doRequest(t, client, http.MethodGet, url+"?dns="+base64.RawURLEncoding.EncodeToString(wire), "", nil)
	defer func() { _ = get.Body.Close() }()
	require.Equal(t, http.StatusOK, get.StatusCode)
	getAnswer := new(mdns.Msg)
	require.NoError(t, getAnswer.Unpack(readAll(t, get.Body)))
	require.Len(t, getAnswer.Answer, 1)
	ga, ok := getAnswer.Answer[0].(*mdns.A)
	require.True(t, ok)
	require.Equal(t, netip.MustParseAddr("203.0.113.8").AsSlice(), []byte(ga.A),
		"DoH GET answers what plaintext answers")

	bad := doRequest(t, client, http.MethodPost, url, "application/dns-message", []byte("not dns"))
	defer func() { _ = bad.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, bad.StatusCode, "an unparseable message is a bad request")

	other := doRequest(t, client, http.MethodPost, url, "text/plain", wire)
	defer func() { _ = other.Body.Close() }()
	require.Equal(t, http.StatusUnsupportedMediaType, other.StatusCode, "only the DNS media type is accepted")

	stray := doRequest(t, client, http.MethodPut, url, "application/dns-message", wire)
	require.NoError(t, err)
	defer func() { _ = stray.Body.Close() }()
	require.Equal(t, http.StatusMethodNotAllowed, stray.StatusCode, "RFC 8484 defines GET and POST only")

	tlsAsks := 0
	for _, addr := range identity.asked() {
		if addr == netip.MustParseAddr("127.0.0.1") {
			tlsAsks++
		}
	}
	// The POST and the GET reach the handler; the malformed requests stop at
	// the DoH boundary and never spend the pipeline.
	require.Equal(t, 2, tlsAsks, "every encrypted query reaches the handler identified")
}

func TestStartRejectsATLSListenerWithoutACertificate(t *testing.T) {
	_, err := dns.Start(dns.ServerConfig{
		Handler:    &dns.Handler{},
		Address:    "127.0.0.1:0",
		DoTAddress: "127.0.0.1:0",
	})
	require.Error(t, err, "a DoT listener without a certificate cannot serve")

	_, err = dns.Start(dns.ServerConfig{
		Handler:    &dns.Handler{},
		Address:    "127.0.0.1:0",
		DoHAddress: "127.0.0.1:0",
	})
	require.Error(t, err, "a DoH listener without a certificate cannot serve")
}

func TestStartBindsNoTLSListenersWithoutAddresses(t *testing.T) {
	upstream := resolverFunc(func(_ context.Context, req *mdns.Msg, _ string) (*mdns.Msg, error) {
		return upstreamA(req, "203.0.113.9"), nil
	})
	handler := handlerFor(t, deciderFor(t, defaultPolicy), upstream)

	server, err := dns.Start(dns.ServerConfig{Handler: handler, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })

	require.Nil(t, server.DoTAddr(), "no DoT address configured, no DoT socket")
	require.Nil(t, server.DoHAddr(), "no DoH address configured, no DoH socket")

	resp, _, err := new(mdns.Client{Net: "udp", Timeout: 2 * time.Second}).
		Exchange(query("example.net.", mdns.TypeA), server.UDPAddr().String())
	require.NoError(t, err)
	require.NotEmpty(t, resp.Answer, "the plaintext listeners keep serving")
}
