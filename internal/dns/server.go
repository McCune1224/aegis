package dns

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	mdns "github.com/miekg/dns"
)

// doHMediaType is the RFC 8484 media type for a DNS message, on the way in
// and on the way out.
const doHMediaType = "application/dns-message"

// maxDoHMessage bounds one request body: a DNS message cannot cross 64KiB on
// any transport, so a larger body is a broken client, not a query.
const maxDoHMessage = 1 << 16

// ServerConfig is what Start needs. DoTAddress and DoHAddress enable the
// encrypted listeners; both are empty unless an operator turns them on, and
// either one requires TLS.
type ServerConfig struct {
	Handler    *Handler
	Address    string
	DoTAddress string
	DoHAddress string
	TLS        *tls.Config
	Logger     *slog.Logger
}

// Server serves DNS over UDP, TCP, and, when configured, DoT and DoH. It
// owns the sockets, so shutdown closes them rather than relying on miekg/dns
// to track whether it started.
type Server struct {
	packet  net.PacketConn
	stream  net.Listener
	dot     net.Listener
	doh     net.Listener
	dohHTTP *http.Server
	serving sync.WaitGroup
}

// Start binds address for UDP and TCP and begins serving. A caller that passes
// port 0 reads the ports the kernel chose from UDPAddr and TCPAddr as soon as
// Start returns. The sockets are bound before Start returns, so a query that
// arrives immediately waits in the socket buffer rather than being lost.
func Start(cfg ServerConfig) (*Server, error) {
	if cfg.Handler == nil {
		return nil, errors.New("dns: ServerConfig.Handler is required")
	}
	if cfg.Address == "" {
		return nil, errors.New("dns: ServerConfig.Address is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var listen net.ListenConfig
	packet, err := listen.ListenPacket(context.Background(), "udp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("dns: %w", err)
	}
	stream, err := listen.Listen(context.Background(), "tcp", cfg.Address)
	if err != nil {
		_ = packet.Close()
		return nil, fmt.Errorf("dns: %w", err)
	}

	s := &Server{packet: packet, stream: stream}
	serve := &serveDNS{handler: cfg.Handler, logger: logger}
	udp := &mdns.Server{PacketConn: packet, Handler: serve}
	tcp := &mdns.Server{Listener: stream, Handler: serve}

	for _, run := range []func() error{udp.ActivateAndServe, tcp.ActivateAndServe} {
		s.startServing(logger, run)
	}

	for _, pair := range []struct {
		name    string
		address string
	}{
		{"DoT", cfg.DoTAddress},
		{"DoH", cfg.DoHAddress},
	} {
		if pair.address == "" {
			continue
		}
		if cfg.TLS == nil {
			_ = packet.Close()
			_ = stream.Close()
			return nil, fmt.Errorf("dns: a %s listener needs ServerConfig.TLS", pair.name)
		}
		listener, err := listen.Listen(context.Background(), "tcp", pair.address)
		if err != nil {
			_ = s.Shutdown(context.Background())
			return nil, fmt.Errorf("dns: %s: %w", pair.name, err)
		}
		listener = tls.NewListener(listener, cfg.TLS)
		if pair.name == "DoT" {
			s.dot = listener
			tlsServer := &mdns.Server{Listener: listener, Handler: serve}
			s.startServing(logger, tlsServer.ActivateAndServe)
			continue
		}
		s.doh = listener
		s.dohHTTP = &http.Server{Handler: dohHandler(cfg.Handler, logger), ReadHeaderTimeout: 10 * time.Second}
		s.startServing(logger, func() error { return s.dohHTTP.Serve(listener) })
	}

	return s, nil
}

func (s *Server) startServing(logger *slog.Logger, run func() error) {
	s.serving.Add(1)
	go func() {
		defer s.serving.Done()
		if err := run(); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("dns: listener stopped", "error", err)
		}
	}()
}

// UDPAddr is the address the UDP listener bound.
func (s *Server) UDPAddr() net.Addr { return s.packet.LocalAddr() }

// TCPAddr is the address the TCP listener bound.
func (s *Server) TCPAddr() net.Addr { return s.stream.Addr() }

// DoTAddr is the address the DoT listener bound, or nil when disabled.
func (s *Server) DoTAddr() net.Addr {
	if s.dot == nil {
		return nil
	}
	return s.dot.Addr()
}

// DoHAddr is the address the DoH listener bound, or nil when disabled.
func (s *Server) DoHAddr() net.Addr {
	if s.doh == nil {
		return nil
	}
	return s.doh.Addr()
}

// Shutdown closes both sockets and waits for the serve loops to return. It is
// safe to call more than once. A query in flight when Shutdown runs may be
// dropped and retried by the client, which is the trade for not holding DNS
// workers open during a restart.
func (s *Server) Shutdown(ctx context.Context) error {
	err := errors.Join(s.packet.Close(), s.stream.Close())
	if s.dot != nil {
		err = errors.Join(err, s.dot.Close())
	}
	if s.dohHTTP != nil {
		herr := s.dohHTTP.Shutdown(ctx)
		if !errors.Is(herr, http.ErrServerClosed) {
			err = errors.Join(err, herr)
		}
	}
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}

	stopped := make(chan struct{})
	go func() {
		s.serving.Wait()
		close(stopped)
	}()

	select {
	case <-stopped:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// serveDNS keeps the transport concerns, writing the reply and reporting what
// failed, out of Handler, which only decides.
type serveDNS struct {
	handler *Handler
	logger  *slog.Logger
}

func (s *serveDNS) ServeDNS(w mdns.ResponseWriter, req *mdns.Msg) {
	resp, err := s.handler.Handle(context.Background(), req, remoteAddress(w))
	if err != nil {
		s.logger.Warn("dns: query failed", "error", err, "client", w.RemoteAddr().String())
	}
	if resp == nil {
		return
	}
	if err := w.WriteMsg(resp); err != nil {
		s.logger.Warn("dns: write failed", "error", err, "client", w.RemoteAddr().String())
	}
}

// remoteAddress turns the transport address into a client address. It unmaps a
// v4-mapped v6 address so that an IPv4 client matches the IPv4 address an
// operator configured for it.
func remoteAddress(w mdns.ResponseWriter) netip.Addr {
	switch addr := w.RemoteAddr().(type) {
	case *net.UDPAddr:
		return addr.AddrPort().Addr().Unmap()
	case *net.TCPAddr:
		return addr.AddrPort().Addr().Unmap()
	default:
		return netip.Addr{}
	}
}

// dohHandler answers RFC 8484 queries on the handler every other transport
// uses. A GET carries the message as an unpadded base64url query parameter; a
// POST carries it as the body. The client address travels the same as on any
// other TCP listener, so per-client policy and rate limits hold.
func dohHandler(handler *Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := readDoHMessage(w, r)
		if !ok {
			return
		}
		req := new(mdns.Msg)
		if err := req.Unpack(raw); err != nil {
			http.Error(w, "unparseable DNS message", http.StatusBadRequest)
			return
		}
		resp, err := handler.Handle(r.Context(), req, requestAddress(r))
		if err != nil || resp == nil {
			logger.Warn("dns: DoH query failed", "error", err, "client", r.RemoteAddr)
			http.Error(w, "upstream failure", http.StatusBadGateway)
			return
		}
		payload, err := resp.Pack()
		if err != nil {
			http.Error(w, "unpackable answer", http.StatusInternalServerError)
			return
		}
		if ttl, ok := shortestAnswerTTL(resp); ok {
			w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", ttl))
		}
		w.Header().Set("Content-Type", doHMediaType)
		_, _ = w.Write(payload)
	})
}

// readDoHMessage pulls the wire-format query out of a GET or a POST and
// reports failures the RFC prescribes: 405 for any other method, 415 for a
// POST that is not a DNS message, 400 for one that cannot be read.
func readDoHMessage(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query().Get("dns")
		if q == "" {
			http.Error(w, "missing dns parameter", http.StatusBadRequest)
			return nil, false
		}
		raw, err := base64.RawURLEncoding.DecodeString(q)
		if err != nil {
			raw, err = base64.URLEncoding.DecodeString(q)
		}
		if err != nil || len(raw) == 0 {
			http.Error(w, "dns parameter is not base64url", http.StatusBadRequest)
			return nil, false
		}
		return raw, true
	case http.MethodPost:
		if !strings.HasPrefix(r.Header.Get("Content-Type"), doHMediaType) {
			http.Error(w, "content type must be "+doHMediaType, http.StatusUnsupportedMediaType)
			return nil, false
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDoHMessage))
		if err != nil || len(raw) == 0 {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return nil, false
		}
		return raw, true
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "RFC 8484 defines GET and POST", http.StatusMethodNotAllowed)
		return nil, false
	}
}

// requestAddress turns the HTTP remote address into a client address, the
// same unmap every other transport applies.
func requestAddress(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// shortestAnswerTTL is the smallest TTL in the answer section, the value a
// caching client may hold the response for (RFC 8484 section 5.1).
func shortestAnswerTTL(resp *mdns.Msg) (uint32, bool) {
	var shortest uint32
	var found bool
	for _, rr := range resp.Answer {
		ttl := rr.Header().Ttl
		if !found || ttl < shortest {
			shortest, found = ttl, true
		}
	}
	return shortest, found
}
