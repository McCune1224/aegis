package dns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	mdns "github.com/miekg/dns"
)

// ServerConfig is what Start needs.
type ServerConfig struct {
	Handler *Handler
	Address string
	Logger  *slog.Logger
}

// Server serves DNS over UDP and TCP. It owns both sockets, so shutdown closes
// them rather than relying on miekg/dns to track whether it started.
type Server struct {
	packet  net.PacketConn
	stream  net.Listener
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
		return nil, fmt.Errorf("dns: listen udp %s: %w", cfg.Address, err)
	}
	stream, err := listen.Listen(context.Background(), "tcp", cfg.Address)
	if err != nil {
		_ = packet.Close()
		return nil, fmt.Errorf("dns: listen tcp %s: %w", cfg.Address, err)
	}

	s := &Server{packet: packet, stream: stream}
	serve := &serveDNS{handler: cfg.Handler, logger: logger}
	udp := &mdns.Server{PacketConn: packet, Handler: serve}
	tcp := &mdns.Server{Listener: stream, Handler: serve}

	for _, run := range []func() error{udp.ActivateAndServe, tcp.ActivateAndServe} {
		s.serving.Add(1)
		go func() {
			defer s.serving.Done()
			if err := run(); err != nil && !errors.Is(err, net.ErrClosed) {
				logger.Warn("dns: listener stopped", "error", err)
			}
		}()
	}

	return s, nil
}

// UDPAddr is the address the UDP listener bound.
func (s *Server) UDPAddr() net.Addr { return s.packet.LocalAddr() }

// TCPAddr is the address the TCP listener bound.
func (s *Server) TCPAddr() net.Addr { return s.stream.Addr() }

// Shutdown closes both sockets and waits for the serve loops to return. It is
// safe to call more than once. A query in flight when Shutdown runs may be
// dropped and retried by the client, which is the trade for not holding DNS
// workers open during a restart.
func (s *Server) Shutdown(ctx context.Context) error {
	err := errors.Join(s.packet.Close(), s.stream.Close())
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
	resp, err := s.handler.Handle(context.Background(), req)
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
