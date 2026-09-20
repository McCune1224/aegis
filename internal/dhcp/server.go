package dhcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/net/ipv4"

	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/store"
)

// Config is what one DHCP server serves.
type Config struct {
	// Address is the UDP address to listen on, as host:port. Port 67 is the
	// DHCP server port; the tests give it an ephemeral one.
	Address string
	Pool    Pool
	// ServerIP is the address clients are told to renew with, and Router is
	// the address they are told to send their traffic to.
	ServerIP netip.Addr
	Router   netip.Addr
	// DNS is the resolver list in the lease, which is the point of running this
	// next to a sinkhole: the device is told to ask the sinkhole.
	DNS       []netip.Addr
	LeaseTime time.Duration
	// Identity answers which client a device belongs to, from the address and
	// hardware address a request carries. An empty key means no client record
	// claims the device, which is what makes a discovery.
	Identity func(client.Selector) filter.ClientKey
}

// sweepPeriod is how often the server drops the leases that have ended.
const sweepPeriod = time.Minute

// broadcast is where a client with no address of its own is answered.
var broadcast = netip.MustParseAddr("255.255.255.255")

// Server answers DHCPv4 on one socket and keeps its leases in the store.
type Server struct {
	config   Config
	database *store.Store
	leases   *client.Dynamic
	logger   *slog.Logger

	mu    sync.Mutex
	table map[netip.Addr]store.Lease
	byMAC map[string]netip.Addr

	now  func() time.Time
	conn net.PacketConn
	// control names the interface a request arrived on, which a reply to the
	// limited broadcast address needs: 255.255.255.255 has no route, so the
	// kernel has to be told where to put it.
	control *ipv4.PacketConn
}

// New returns a server. A nil logger uses the default.
func New(config Config, database *store.Store, leases *client.Dynamic, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		config:   config,
		database: database,
		leases:   leases,
		logger:   logger,
		table:    make(map[netip.Addr]store.Lease),
		byMAC:    make(map[string]netip.Addr),
		now:      time.Now,
	}
}

// Load reads the stored leases into the request path and drops the ones that
// have already ended, so a restart resumes the leases a device is still using.
func (s *Server) Load(ctx context.Context) error {
	if _, err := s.database.ExpireLeases(ctx, s.now()); err != nil {
		return err
	}
	stored, err := s.database.Leases(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, lease := range stored {
		if !lease.Expires.After(s.now()) {
			continue
		}
		s.remember(lease)
	}
	return nil
}

// Serve listens and answers until the context ends.
func (s *Server) Serve(ctx context.Context) error {
	var listen net.ListenConfig
	listen.Control = broadcastControl
	conn, err := listen.ListenPacket(ctx, "udp4", s.config.Address)
	if err != nil {
		return fmt.Errorf("dhcp: listen on %s: %w", s.config.Address, err)
	}
	return s.ServeConn(ctx, conn)
}

// ServeConn answers on an already-open socket, which is how the tests put the
// server on an ephemeral port.
func (s *Server) ServeConn(ctx context.Context, conn net.PacketConn) error {
	control := controlConn(conn)

	s.mu.Lock()
	s.conn = conn
	s.control = control
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		_ = conn.Close()
	}()

	sweeper := make(chan struct{})
	go s.sweepLoop(ctx, sweeper)
	defer close(sweeper)

	buffer := make([]byte, 1500)
	for {
		read, from, iface, err := readPacket(conn, control, buffer)
		if err != nil {
			select {
			case <-ctx.Done():
				<-done
				return nil
			default:
				return fmt.Errorf("dhcp: read: %w", err)
			}
		}
		packet := make([]byte, read)
		copy(packet, buffer[:read])
		s.handle(ctx, packet, from, iface)
	}
}

// controlConn turns a UDP socket into one that reports the interface a packet
// arrived on. A socket that is not one, or a platform that cannot say, leaves
// the server sending on whatever route the kernel picks.
func controlConn(conn net.PacketConn) *ipv4.PacketConn {
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		return nil
	}
	control := ipv4.NewPacketConn(udp)
	if err := control.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		return nil
	}
	return control
}

func readPacket(conn net.PacketConn, control *ipv4.PacketConn, buffer []byte) (int, net.Addr, int, error) {
	if control == nil {
		read, from, err := conn.ReadFrom(buffer)
		return read, from, 0, err
	}
	read, message, from, err := control.ReadFrom(buffer)
	if message == nil {
		return read, from, 0, err
	}
	return read, from, message.IfIndex, err
}

// Close stops the server. It is safe on a server that never served, which is
// what a disabled configuration leaves behind.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *Server) sweepLoop(ctx context.Context, done <-chan struct{}) {
	ticker := time.NewTicker(sweepPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
				s.logger.Warn("dhcp: the lease sweep failed", "error", err)
			}
		}
	}
}

// Sweep drops the leases that have ended and reports how many rows went.
func (s *Server) Sweep(ctx context.Context) (int64, error) {
	now := s.now()

	s.mu.Lock()
	for address, lease := range s.table {
		if !lease.Expires.After(now) {
			delete(s.table, address)
			delete(s.byMAC, client.NormalizeMAC(lease.MAC))
			s.leases.Forget(address)
		}
	}
	s.mu.Unlock()

	return s.database.ExpireLeases(ctx, now)
}

// handle answers one packet. Everything the server decides is here, so a
// request's whole path is readable in one place.
func (s *Server) handle(ctx context.Context, packet []byte, from net.Addr, iface int) {
	message, err := Parse(packet)
	if err != nil {
		s.logger.Debug("dhcp: ignored a packet", "error", err, "from", from)
		return
	}
	if message.Op != OpRequest {
		return
	}

	switch message.Type {
	case Discover:
		s.offer(ctx, message, from, iface)
	case Request:
		s.handleRequest(ctx, message, from, iface)
	case Release:
		s.release(ctx, message.ClientIP)
	case Decline:
		s.decline(ctx, message.RequestedIP)
	case Inform, Offer, Ack, Nak:
		// Nothing to say to these.
	}
}

// offer answers a discover, or stays quiet when the pool has nothing left: a
// client that gets no offer tries again, which is better than a reply that
// grants it nothing.
func (s *Server) offer(ctx context.Context, message Message, from net.Addr, iface int) {
	address := s.pick(message.MAC, message.RequestedIP)
	if !address.IsValid() {
		s.logger.Warn("dhcp: the pool has no free address", "client", message.MAC)
		return
	}
	s.answer(ctx, message, from, iface, Offer, address)
}

func (s *Server) handleRequest(ctx context.Context, message Message, from net.Addr, iface int) {
	// A client that accepted another server names it, and this one must stay
	// quiet so the two do not both answer.
	if message.ServerID.IsValid() && message.ServerID != s.config.ServerIP {
		return
	}

	wanted := message.RequestedIP
	if !wanted.IsValid() {
		wanted = message.ClientIP
	}
	held, taken := s.holder(wanted)
	if !s.config.Pool.Contains(wanted) || (taken && client.NormalizeMAC(held.MAC) != client.NormalizeMAC(message.MAC)) {
		s.answer(ctx, message, from, iface, Nak, netip.Addr{})
		return
	}

	s.answer(ctx, message, from, iface, Ack, wanted)
}

// answer sends one reply and, for the two types that grant an address, records
// the lease. The address is committed before the packet goes out, so a reply
// the client acts on is one the server also believes.
func (s *Server) answer(ctx context.Context, message Message, from net.Addr, iface int, kind Type, address netip.Addr) {
	// The offer holds the address, so two devices asking at once cannot be
	// promised the same one; the ack confirms it.
	if kind == Offer || kind == Ack {
		s.commit(ctx, message.MAC, address, message.Hostname)
	}

	reply := Reply{
		Type:       kind,
		XID:        message.XID,
		Broadcast:  message.Broadcast,
		MAC:        message.MAC,
		ServerIP:   s.config.ServerIP,
		SubnetMask: s.config.Pool.Network,
		Router:     s.config.Router,
		DNS:        s.config.DNS,
		LeaseTime:  s.config.LeaseTime,
		Hostname:   message.Hostname,
	}
	if kind != Nak {
		reply.YourIP = address
	}

	target := s.replyTarget(message, from)
	if err := s.send(reply.Marshal(), target, iface); err != nil && ctx.Err() == nil {
		s.logger.Warn("dhcp: a reply did not go out", "error", err, "to", target)
	}
}

// send writes one reply, naming the interface it must leave by when the kernel
// needs telling.
func (s *Server) send(packet []byte, target netip.AddrPort, iface int) error {
	s.mu.Lock()
	conn, control := s.conn, s.control
	s.mu.Unlock()
	if conn == nil {
		return errors.New("dhcp: the server is not serving")
	}

	address := net.UDPAddrFromAddrPort(target)
	if control != nil && iface != 0 {
		if _, err := control.WriteTo(packet, &ipv4.ControlMessage{IfIndex: iface}, address); err == nil {
			return nil
		}
	}
	_, err := conn.WriteTo(packet, address)
	return err
}

// replyTarget is where one reply goes. A relayed request goes back through the
// relay. A client that reached the server from a real address — a renewal, or a
// test on loopback — is answered there. A client with no address of its own can
// only hear a broadcast.
func (s *Server) replyTarget(message Message, from net.Addr) netip.AddrPort {
	if message.GatewayIP.IsValid() {
		return netip.AddrPortFrom(message.GatewayIP, 67)
	}
	if source, ok := from.(*net.UDPAddr); ok {
		if address, ok := netip.AddrFromSlice(source.IP); ok {
			address = address.Unmap()
			if address.IsValid() && !address.IsUnspecified() && !address.IsMulticast() {
				return netip.AddrPortFrom(address, uint16(source.Port))
			}
		}
	}
	if message.Broadcast || !message.ClientIP.IsValid() {
		return netip.AddrPortFrom(broadcast, 68)
	}
	return netip.AddrPortFrom(message.ClientIP, 68)
}

// pick chooses the address one device should hold: the one it already holds,
// then the one it asked for when the pool can give it, then the first free
// address. It returns an invalid address when the pool has nothing left.
func (s *Server) pick(hardware net.HardwareAddr, requested netip.Addr) netip.Addr {
	if held := s.addressFor(hardware); held.IsValid() {
		return held
	}
	if _, taken := s.holder(requested); s.config.Pool.Contains(requested) && !taken {
		return requested
	}

	var free netip.Addr
	s.config.Pool.Addresses(func(address netip.Addr) bool {
		if _, taken := s.holder(address); !taken {
			free = address
			return false
		}
		return true
	})
	return free
}

// commit records a granted address in the request path, in the DNS identity
// table, and in the store. A device no client record claims becomes a
// discovery, which is the prompt the operator answers.
func (s *Server) commit(ctx context.Context, hardware net.HardwareAddr, address netip.Addr, hostname string) {
	if !address.IsValid() {
		return
	}
	now := s.now()

	lease := store.Lease{
		Address:  address,
		MAC:      hardware,
		Hostname: hostname,
		Expires:  now.Add(s.config.LeaseTime),
	}
	if s.config.Identity != nil {
		lease.Client = s.config.Identity(client.Selector{Address: address, MAC: hardware})
	}

	s.mu.Lock()
	previous, had := s.table[address]
	s.remember(lease)
	s.mu.Unlock()
	if had && previous.Client != "" && previous.Client != lease.Client {
		s.logger.Info("dhcp: an address changed hands", "address", address, "from", previous.Client, "to", lease.Client)
	}

	if err := s.database.SaveLease(ctx, lease); err != nil && ctx.Err() == nil {
		s.logger.Warn("dhcp: the lease was granted but not stored", "address", address, "error", err)
	}

	if lease.Client == "" {
		s.discover(ctx, lease, now)
		return
	}
	if err := s.database.DeleteDiscovery(ctx, hardware); err != nil && ctx.Err() == nil {
		s.logger.Warn("dhcp: a discovery outlived the client that claimed it", "error", err)
	}
}

// discover records the device as one no client claims, keeping the first
// sighting so the prompt can say how long it has been around.
func (s *Server) discover(ctx context.Context, lease store.Lease, now time.Time) {
	first := now
	if existing, err := s.database.Discoveries(ctx); err == nil {
		for _, discovery := range existing {
			if client.NormalizeMAC(discovery.MAC) == client.NormalizeMAC(lease.MAC) {
				first = discovery.First
				break
			}
		}
	}

	discovery := store.Discovery{
		MAC:      lease.MAC,
		Address:  lease.Address,
		Hostname: lease.Hostname,
		First:    first,
		Last:     now,
	}
	if err := s.database.RecordDiscovery(ctx, discovery); err != nil && ctx.Err() == nil {
		s.logger.Warn("dhcp: a discovery was not recorded", "error", err)
	}
}

func (s *Server) release(ctx context.Context, address netip.Addr) {
	s.forget(ctx, address)
}

func (s *Server) decline(ctx context.Context, address netip.Addr) {
	s.forget(ctx, address)
}

// forget drops a lease the client gave back, or refused because something on
// the network already answers on it.
func (s *Server) forget(ctx context.Context, address netip.Addr) {
	if !address.IsValid() {
		return
	}

	s.mu.Lock()
	lease, held := s.table[address]
	delete(s.table, address)
	if held {
		delete(s.byMAC, client.NormalizeMAC(lease.MAC))
	}
	s.mu.Unlock()
	s.leases.Forget(address)

	if err := s.database.DeleteLease(ctx, address); err != nil && ctx.Err() == nil {
		s.logger.Warn("dhcp: a released lease stayed in the store", "address", address, "error", err)
	}
}

// remember adds a lease to the request path and to the DNS identity table. The
// caller holds the lock.
func (s *Server) remember(lease store.Lease) {
	s.table[lease.Address] = lease
	s.byMAC[client.NormalizeMAC(lease.MAC)] = lease.Address
	s.leases.Set(lease.Address, lease.MAC)
}

// holder returns the lease for one address and whether it is still live, so the
// request path never hands out an address a row says expired.
func (s *Server) holder(address netip.Addr) (store.Lease, bool) {
	if !address.IsValid() {
		return store.Lease{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, held := s.table[address]
	if !held || !lease.Expires.After(s.now()) {
		return store.Lease{}, false
	}
	return lease, true
}

// addressFor returns the address one device holds, when it still holds one.
func (s *Server) addressFor(hardware net.HardwareAddr) netip.Addr {
	normalized := client.NormalizeMAC(hardware)
	if normalized == "" {
		return netip.Addr{}
	}
	s.mu.Lock()
	address, held := s.byMAC[normalized]
	s.mu.Unlock()
	if !held {
		return netip.Addr{}
	}
	if _, live := s.holder(address); live {
		return address
	}
	return netip.Addr{}
}
